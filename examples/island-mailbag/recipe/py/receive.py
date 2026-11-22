"""Receiving side (Store B): an independent reader and content-addressed store, in Python.

Nothing here imports or reads anything from the sender. It trusts only bytes in a CAR and re-derives every CID itself
with hashlib (SHA-256). CAR framing is read by carbox 0.3 (pure Python over multiformats and dag-cbor); CIDs and DAG-CBOR
are decoded by libipld (Rust bindings). None of it is the JavaScript stack that wrote the files.
(libipld.decode_car was tried first and rejects raw blocks; see reports/libipld-decode-car-probe.txt.)

  receive.py ingest  --car CAR --store DB --report OUT.json   verify every block, then store them in one transaction
  receive.py have    --store DB --out FILE                    CIDs this store holds (what a sender may withhold)
  receive.py restore --root CID --store DB --out DIR          rebuild an edition from verified blocks
  receive.py decode  --car CAR --report OUT.json              decode and verify a CAR without storing anything
  receive.py dump    --store DB --out FILE                    the store's blocks and arrivals as JSON

Store B layout: SQLite, tables blocks(cid, codec, size, sha256, data) and arrivals(seq, car, cid, duplicate).
Exit codes: 0 ok, 3 verification failed (nothing stored), 4 a block needed for a restore is missing.
"""
import argparse, hashlib, json, os, sqlite3, sys

import libipld
from carbox.car import read_car as carbox_read_car

RAW, DAG_CBOR, SHA2_256 = 0x55, 0x71, 0x12
CODEC_NAMES = {RAW: "raw", DAG_CBOR: "dag-cbor"}


class Rejected(Exception):
    pass


def derive_cid(codec, data):
    """The CIDv1 text of `data`, built from the specification's bytes: 0x01, codec, 0x12 0x20, SHA-256."""
    return libipld.encode_cid(bytes([1, codec, SHA2_256, 32]) + hashlib.sha256(data).digest())


def check_block(cid_text, data):
    """Verify one block. Returns its facts, or raises Rejected."""
    cid = libipld.decode_cid(cid_text)
    if cid["version"] != 1:
        raise Rejected(f"{cid_text}: CID version {cid['version']}, expected 1")
    if cid["hash"]["code"] != SHA2_256 or cid["hash"]["size"] != 32:
        raise Rejected(f"{cid_text}: multihash is not sha2-256")
    if cid["codec"] not in CODEC_NAMES:
        raise Rejected(f"{cid_text}: codec 0x{cid['codec']:x} is neither raw nor dag-cbor")
    digest = hashlib.sha256(data).digest()
    if digest != cid["hash"]["digest"]:
        raise Rejected(f"{cid_text}: block bytes hash to {digest.hex()}, CID says {cid['hash']['digest'].hex()}")
    if derive_cid(cid["codec"], data) != cid_text:
        raise Rejected(f"{cid_text}: independently derived CID differs")
    if cid["codec"] == DAG_CBOR:
        # the block must be valid DAG-CBOR and canonical: re-encoding the decoded value gives the same bytes
        value = libipld.decode_dag_cbor(data)
        if libipld.encode_dag_cbor(value) != data:
            raise Rejected(f"{cid_text}: DAG-CBOR is not canonical")
    return {"cid": cid_text, "codec": CODEC_NAMES[cid["codec"]], "bytes": len(data), "sha256": digest.hex()}


def read_car(path):
    data = open(path, "rb").read()
    try:
        roots, blocks = carbox_read_car(data)
    except Exception as e:  # carbox raises ValueError, EOFError and friends on a damaged file
        raise Rejected(f"{os.path.basename(path)}: not a readable CAR: {e}")
    if not roots:
        raise Rejected(f"{os.path.basename(path)}: CAR has no roots")
    text = lambda cid: cid.encode("base32")  # multibase 'b', as every CID in this exhibit is written
    return data, [text(r) for r in roots], {text(b.cid): bytes(b.data) for b in blocks}


def verify_car(path):
    raw_bytes, roots, blocks = read_car(path)
    facts = [check_block(c, d) for c, d in blocks.items()]
    for r in roots:
        if r not in blocks:
            raise Rejected(f"root {r} is not in the CAR")
    return raw_bytes, roots, blocks, facts


def open_store(path):
    db = sqlite3.connect(path)
    db.executescript(
        """CREATE TABLE IF NOT EXISTS blocks(cid TEXT PRIMARY KEY, codec TEXT NOT NULL, size INTEGER NOT NULL,
             sha256 TEXT NOT NULL, data BLOB NOT NULL);
           CREATE TABLE IF NOT EXISTS arrivals(seq INTEGER PRIMARY KEY AUTOINCREMENT, car TEXT NOT NULL,
             cid TEXT NOT NULL, duplicate INTEGER NOT NULL);"""
    )
    return db


def get_verified(db, cid_text):
    row = db.execute("SELECT data FROM blocks WHERE cid = ?", (cid_text,)).fetchone()
    if row is None:
        raise KeyError(cid_text)
    check_block(cid_text, bytes(row[0]))  # verified again on every read
    return bytes(row[0])


def cmd_ingest(a):
    name = os.path.basename(a.car)
    report = {"reader": f"{reader_label()}", "car": name, "accepted": False}
    try:
        raw_bytes, roots, blocks, facts = verify_car(a.car)
    except Rejected as e:
        report["error"] = str(e)
        write(a.report, report)
        print(f"REJECTED {name}: {e}")
        return 3
    db = open_store(a.store)
    new = dup = 0
    with db:  # one transaction: all blocks or none
        for f in facts:
            have = db.execute("SELECT 1 FROM blocks WHERE cid = ?", (f["cid"],)).fetchone()
            if have is None:
                db.execute("INSERT INTO blocks VALUES (?,?,?,?,?)", (f["cid"], f["codec"], f["bytes"], f["sha256"], blocks[f["cid"]]))
                new += 1
            else:
                dup += 1
            db.execute("INSERT INTO arrivals(car, cid, duplicate) VALUES (?,?,?)", (name, f["cid"], 1 if have else 0))
    total = db.execute("SELECT count(*) FROM blocks").fetchone()[0]
    report.update(accepted=True, carBytes=len(raw_bytes), carSha256=hashlib.sha256(raw_bytes).hexdigest(), carVersion=1, roots=roots,
                  blocks=facts, newBlocks=new, duplicateBlocks=dup, storeBlocksNow=total)
    write(a.report, report)
    print(f"accepted {name}: {len(facts)} blocks verified, {new} new, {dup} already held, store now holds {total}")
    return 0


def cmd_have(a):
    db = open_store(a.store)
    cids = sorted(r[0] for r in db.execute("SELECT cid FROM blocks"))
    open(a.out, "w").write("".join(c + "\n" for c in cids))
    print(f"{len(cids)} CIDs held")
    return 0


def cmd_restore(a):
    db = open_store(a.store)
    try:
        root = get_verified(db, a.root)
    except KeyError:
        print(f"MISSING root block {a.root}")
        return 4
    except Rejected as e:
        print(f"REJECTED {e}")
        return 3
    m = libipld.decode_dag_cbor(root)
    if m.get("schema") != "island-mailbag/edition/v1":
        print(f"REJECTED {a.root}: unknown schema {m.get('schema')!r}")
        return 3
    fetched, missing = [], []
    for f in m["files"]:
        link = str(f["link"])
        try:
            data = get_verified(db, link)
        except KeyError:
            missing.append({"name": f["name"], "cid": link})
            continue
        if len(data) != f["size"]:
            print(f"REJECTED {f['name']}: manifest says {f['size']} bytes, block has {len(data)}")
            return 3
        fetched.append((f, link, data))
    if missing:  # nothing is written for an edition that cannot be completed
        for x in missing:
            print(f"MISSING block {x['cid']} ({x['name']})")
        return 4
    os.makedirs(a.out, exist_ok=True)
    files = []
    for f, link, data in fetched:
        open(os.path.join(a.out, f["name"]), "wb").write(data)
        files.append({"name": f["name"], "mediaType": f["mediaType"], "bytes": len(data), "cid": link,
                      "sha256": hashlib.sha256(data).hexdigest()})
    # companions for readers that have no CID library: the root, the manifest as JSON, and a checksum list
    # derived from the CIDs' multihash digests, not from the restored files
    base = os.path.dirname(a.out.rstrip("/"))
    stem = os.path.basename(a.out.rstrip("/"))
    meta = {"root": a.root, "schema": m["schema"], "library": m["library"], "edition": m["edition"], "files": files}
    write(os.path.join(base, stem + ".manifest.json"), meta)
    open(os.path.join(base, stem + ".ROOT"), "w").write(a.root + "\n")
    open(os.path.join(base, stem + ".SHA256SUMS"), "w").write(
        "".join(f"{libipld.decode_cid(f['cid'])['hash']['digest'].hex()}  {f['name']}\n" for f in files))
    print(f"restored edition {m['edition']} ({len(files)} files) from root {a.root}")
    return 0


def cmd_decode(a):
    report = {"reader": f"{reader_label()}", "file": os.path.basename(a.car)}
    try:
        raw_bytes, roots, blocks, facts = verify_car(a.car)
    except Rejected as e:
        report.update(error=str(e), allHashesOk=False)
        write(a.report, report)
        print(f"REJECTED {report['file']}: {e}")
        return 3
    report.update(bytes=len(raw_bytes), sha256=hashlib.sha256(raw_bytes).hexdigest(), carVersion=1, roots=roots,
                  blocks=[{"cid": f["cid"], "codec": f["codec"], "bytes": f["bytes"], "hashOk": True} for f in facts], allHashesOk=True)
    write(a.report, report)
    print(f"{report['file']}: {len(facts)} blocks, hashes ok")
    return 0


def cmd_dump(a):
    db = open_store(a.store)
    write(a.out, {
        "store": "Store B: SQLite via Python sqlite3",
        "blocks": [{"cid": c, "codec": k, "bytes": s, "sha256": h} for c, k, s, h in db.execute("SELECT cid, codec, size, sha256 FROM blocks ORDER BY cid")],
        "arrivals": [{"seq": q, "car": c, "cid": i, "duplicate": bool(d)} for q, c, i, d in db.execute("SELECT seq, car, cid, duplicate FROM arrivals ORDER BY seq")],
    })
    return 0


def reader_label():
    import importlib.metadata as m
    return f"carbox {m.version('carbox')} + libipld {m.version('libipld')} + hashlib (Python {sys.version.split()[0]})"


def write(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2)
        f.write("\n")


p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
sub = p.add_subparsers(dest="cmd", required=True)
for name, fn, opts in [
    ("ingest", cmd_ingest, ["car", "store", "report"]),
    ("have", cmd_have, ["store", "out"]),
    ("restore", cmd_restore, ["root", "store", "out"]),
    ("decode", cmd_decode, ["car", "report"]),
    ("dump", cmd_dump, ["store", "out"]),
]:
    s = sub.add_parser(name)
    for o in opts:
        s.add_argument("--" + o, required=True)
    s.set_defaults(fn=fn)
args = p.parse_args()
sys.exit(args.fn(args))
