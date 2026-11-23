"""verify.py RUN_DIR --epub BOOK [--git-repo DIR]: independent assertions about a replay, from the bytes on disk.
Writes RUN_DIR/checks.json.

--epub      the original four-moons.epub (the file the replay was given)
--git-repo  optional: a Lakeshore-Labs/computer checkout or clone; adds the check that the EPUB is byte-identical to
            `git show 71684674…:examples/moon-almanac/sample/four-moons.epub`. Without it that one check is not run
            (51 checks with it, 50 without), never silently counted as passed.

It does not trust the stage reports for the facts it asserts: it re-reads the CARs with carbox, re-hashes every block with
hashlib, decodes manifests with libipld, reads Store B with sqlite3, and compares files byte for byte.
Exit code 0 only if every check passes.
"""
import hashlib, json, os, pathlib, sqlite3, subprocess, sys
import libipld
from carbox.car import read_car

import argparse
ap = argparse.ArgumentParser()
ap.add_argument("run"); ap.add_argument("--epub", required=True); ap.add_argument("--git-repo")
cli = ap.parse_args()
run = os.path.abspath(cli.run)
J = lambda *p: os.path.join(run, *p)
rd = lambda *p: open(J(*p), "rb").read()
sha = lambda b: hashlib.sha256(b).hexdigest()
checks, obs = [], []


def check(title, ok, detail=""):
    checks.append({"title": title, "pass": bool(ok), "detail": str(detail)})


def car(path):
    roots, blocks = read_car(open(path, "rb").read())
    text = lambda c: c.encode("base32")
    return [text(r) for r in roots], [(text(b.cid), bytes(b.data)) for b in blocks]


def derive(codec, data):
    return libipld.encode_cid(bytes([1, codec, 0x12, 32]) + hashlib.sha256(data).digest())


# ---- the original book -------------------------------------------------------------------------------------------
BOOK_SHA = "db45721e5c875bc1be2b4b0870132c70d04d2e0aa6e29b48729ee771268b8ee5"
COMMIT = "71684674a05273f6c689b5d3c78d8e8f19d8191c"
book = open(cli.epub, "rb").read()
check("original book is 121,382 bytes with the published SHA-256", len(book) == 121382 and sha(book) == BOOK_SHA, sha(book))
if cli.git_repo:
    git = subprocess.run(["git", "-C", cli.git_repo, "show", f"{COMMIT}:examples/moon-almanac/sample/four-moons.epub"], capture_output=True)
    check(f"the input is byte-identical to the book in commit {COMMIT[:12]} (git show)", git.returncode == 0 and git.stdout == book)

# ---- Computer wrote the editions ---------------------------------------------------------------------------------
e1, e2 = {n: rd("island/edition-1", n) for n in ("four-moons.epub", "reading-card.md")}, {n: rd("island/edition-2", n) for n in ("four-moons.epub", "reading-card.md")}
check("edition 1 and edition 2 each hold exactly a book and a card", sorted(os.listdir(J("island/edition-1"))) == sorted(os.listdir(J("island/edition-2"))) == ["four-moons.epub", "reading-card.md"])
check("the book in both editions is byte-identical to the original", e1["four-moons.epub"] == book == e2["four-moons.epub"])
l1, l2 = e1["reading-card.md"].decode().splitlines(), e2["reading-card.md"].decode().splitlines()
diff = sorted(set(l1) ^ set(l2))
fields = sorted({x.split(": ")[0] for x in diff})
check("the cards differ only in the Edition, When and Changed lines", fields == ["Changed", "Edition", "When"], fields)
check("the meeting place is unchanged and the time moved from 10:00 to 14:30",
      "Where: Ferry Landing Reading Room" in l1 and "Where: Ferry Landing Reading Room" in l2
      and "When: Saturday 28 November 2026, 10:00" in l1 and "When: Saturday 28 November 2026, 14:30" in l2)
for n, files in ((1, e1), (2, e2)):
    g = json.load(open(J("island", f"edition-{n}.guest.json")))
    check(f"edition {n}: the digests the guest reported equal ours", {f["name"]: f["sha256"] for f in g["files"]} == {k: sha(v) for k, v in files.items()})
check("Computer's guest wrote the first edition from a read-only shelf (guest-a1 exit 0)", "guest-a1 0" in open(J("reports/guest-exits.txt")).read())

tdir = J("transcripts")
bad = [f for f in sorted(os.listdir(tdir)) if "command not found" in open(os.path.join(tdir, f)).read()]
check("no guest transcript contains 'command not found'", not bad, bad)
check("guest-a2 compared the book with edition 1 and found it identical", "identical: " + BOOK_SHA in open(J("transcripts/guest-a2.txt")).read())

# ---- the CARs, read again from bytes -----------------------------------------------------------------------------
boat = sorted(os.listdir(J("boat")))
check("only three files crossed the boat: the two CARs and the receiver's have-list", boat == ["edition-1.car", "edition-2.delta.car", "receiver-have-after-1.txt"], boat)
r1, b1 = car(J("boat/edition-1.car"))
r2, b2 = car(J("boat/edition-2.delta.car"))
rs, bs = car(J("measure/edition-2.standalone.car"))
allblocks = b1 + b2
check("every block in both CARs hashes to its CID (hashlib) and the CID re-derives from the spec's bytes",
      all(derive(libipld.decode_cid(c)["codec"], d) == c for c, d in allblocks + bs))
check("every CID is version 1 with sha2-256, codec raw (0x55) or dag-cbor (0x71)",
      all((lambda x: x["version"] == 1 and x["hash"]["code"] == 0x12 and x["codec"] in (0x55, 0x71))(libipld.decode_cid(c)) for c, _ in allblocks))
check("CAR 1 has one root and 3 blocks; the delta CAR has one root and 2 blocks; the standalone CAR 3", (len(r1), len(b1), len(r2), len(b2), len(rs), len(bs)) == (1, 3, 1, 2, 1, 3))
check("each root is the first block of its CAR and is DAG-CBOR", r1[0] == b1[0][0] and r2[0] == b2[0][0] and libipld.decode_cid(r1[0])["codec"] == 0x71 == libipld.decode_cid(r2[0])["codec"])
m1 = libipld.decode_dag_cbor(dict(b1)[r1[0]])
m2 = libipld.decode_dag_cbor(dict(b2)[r2[0]])
link = lambda m, n: str(next(f["link"] for f in m["files"] if f["name"] == n))
book1, book2 = link(m1, "four-moons.epub"), link(m2, "four-moons.epub")
card1, card2 = link(m1, "reading-card.md"), link(m2, "reading-card.md")
check("the book has the same CID in both manifests", book1 == book2, book1)
check("the book CID is the raw CIDv1 of the original bytes", book1 == derive(0x55, book))
check("the card CID and the root CID changed", card1 != card2 and r1[0] != r2[0], f"card {card1[:14]}… -> {card2[:14]}…, root {r1[0][:14]}… -> {r2[0][:14]}…")
ids1, ids2 = {c for c, _ in b1}, {c for c, _ in b2}
check("the book block is in CAR 1 and NOT in the delta CAR", book1 in ids1 and book1 not in ids2)
check("the delta CAR carries exactly the new card and the new root", ids2 == {card2, r2[0]})
check("across both CARs 5 blocks were sent, all different; 6 are needed to describe both editions, 1 reused",
      len(b1) + len(b2) == 5 and len(ids1 | ids2) == 5 and len(ids1 | ids2 | {book1}) == 5)
check("a standalone edition-2 CAR (measured, never sent) holds the book too", {c for c, _ in bs} == {r2[0], card2, book1})
check("the manifests' sizes equal the block sizes", all(f["size"] == len(dict(allblocks + bs)[str(f["link"])]) for m in (m1, m2) for f in m["files"]))
check("the manifests are canonical DAG-CBOR (decode then encode gives the same bytes)",
      libipld.encode_dag_cbor(m1) == dict(b1)[r1[0]] and libipld.encode_dag_cbor(m2) == dict(b2)[r2[0]])
check("the card blocks are the card files, byte for byte", dict(b1)[card1] == e1["reading-card.md"] and dict(b2)[card2] == e2["reading-card.md"])
sz = lambda p: os.path.getsize(J(p))
saved = sz("measure/edition-2.standalone.car") - sz("boat/edition-2.delta.car")
check("the book's transfer saved on edition 2 equals standalone minus delta CAR, and exceeds the book's own bytes by only its framing",
      saved >= len(book) and saved - len(book) < 100, f"{saved} bytes saved; book payload {len(book)}; framing {saved - len(book)}")

# ---- Store B ----------------------------------------------------------------------------------------------------
db = sqlite3.connect(pathlib.Path(J("store-b/blocks.sqlite")).as_uri() + "?mode=ro", uri=True)  # as_uri escapes spaces, # and &
stored = {c: bytes(d) for c, d in db.execute("SELECT cid, data FROM blocks")}
check("Store B holds 5 blocks, each hashing to its CID", len(stored) == 5 and all(derive(libipld.decode_cid(c)["codec"], d) == c for c, d in stored.items()))
arr = db.execute("SELECT car, count(*), sum(duplicate) FROM arrivals GROUP BY car ORDER BY min(seq)").fetchall()
check("Store B's arrival log: 3 new blocks from CAR 1, then 2 new from the delta, no duplicates", arr == [("edition-1.car", 3, 0), ("edition-2.delta.car", 2, 0)], arr)
check("Store B's block set equals the union of the two CARs", set(stored) == ids1 | ids2)
check("Store B has no second copy of the book: it arrived once, in CAR 1",
      db.execute("SELECT car FROM arrivals WHERE cid = ?", (book1,)).fetchall() == [("edition-1.car",)])
ls_a = sorted(os.listdir(J("store-a")))
nfiles = sum(len([f for f in fs if f.endswith(".data")]) for _, _, fs in os.walk(J("store-a")))
check("Store A (blockstore-fs, flat files) holds 5 blocks; Store B (SQLite) holds the same 5 CIDs; no path is shared", nfiles == 5 and not os.path.realpath(J("store-a")).startswith(os.path.realpath(J("store-b"))), f"{nfiles} .data files in {len(ls_a)} shard dirs")
sa = json.load(open(J("reports/send-1.json")))
sb = json.load(open(J("reports/send-2.json")))
check("Store A rebuilt each edition from its own blocks before sending", sa["storeA"]["roundTripFromStoreA"] and sb["storeA"]["roundTripFromStoreA"])

# ---- restored editions -------------------------------------------------------------------------------------------
for n, files in ((1, e1), (2, e2)):
    check(f"restored edition {n} is byte-identical to the edition Computer wrote", all(rd("restored", f"edition-{n}", k) == v for k, v in files.items()) and sorted(os.listdir(J("restored", f"edition-{n}"))) == sorted(files))
check("restored edition 2's book is the original, restored from Store B's copy", rd("restored/edition-2/four-moons.epub") == book)
check("the restore wrote each root CID next to its edition", rd("restored/edition-1.ROOT").decode().strip() == r1[0] and rd("restored/edition-2.ROOT").decode().strip() == r2[0])

# ---- negative checks ---------------------------------------------------------------------------------------------
check("a delta with one flipped byte was refused by Store B (exit 3)", rd("negative/ingest-tampered.exit").decode().strip() == "3")
check("…and Store B held exactly what it held before", rd("boat/receiver-have-after-1.txt") == rd("negative/have-after-tamper.txt"))
check("…and the JavaScript reader also flags the flipped byte (exit 3)", rd("negative/js-decode-tampered.exit").decode().strip() == "3" and not json.load(open(J("negative/js-decode-tampered.json")))["allHashesOk"])
check("the delta alone cannot restore edition 2: exit 4, naming the missing book block",
      rd("negative/delta-only.exit").decode().strip() == "4" and ("MISSING block " + book1) in open(J("transcripts/delta-only.txt")).read())
check("…and the failed restore wrote no files", not os.path.exists(J("negative/delta-only-restored")))

# ---- independent decoders agree ----------------------------------------------------------------------------------
for c, ids, root in (("edition-1.car", ids1, r1[0]), ("edition-2.delta.car", ids2, r2[0])):
    js, py = json.load(open(J(f"reports/js-decode-{c}.json"))), json.load(open(J(f"reports/py-decode-{c}.json")))
    check(f"{c}: the JavaScript and Python decoders report the same root, the same CIDs and sizes, and all hashes ok",
          js["roots"] == py["roots"] == [root] and {(b["cid"], b["bytes"]) for b in js["blocks"]} == {(b["cid"], b["bytes"]) for b in py["blocks"]} == {(c_, len(d)) for c_, d in dict(b1 + b2).items() if c_ in ids}
          and js["allHashesOk"] and py["allHashesOk"] and js["sha256"] == py["sha256"] == sha(rd("boat", c)))
probe = open(J("reports/libipld-decode-car-probe.txt")).read()
obs.append({"title": "libipld 4.0.0 decode_car refuses CARs that contain raw blocks", "observed": "Unsupported codec. For now we support only DAG-CBOR (0x71)" in probe, "evidence": "reports/libipld-decode-car-probe.txt", "consequence": "carbox reads the CAR framing; libipld is used for CIDs and DAG-CBOR"})

# ---- the new read-only guest -------------------------------------------------------------------------------------
dec = json.load(open(J("guest-b/out/decision.json")))
check("the new guest passed all of its own checks", dec["all_pass"] and len(dec["checks"]) >= 20, f"{sum(c['pass'] for c in dec['checks'])}/{len(dec['checks'])}")
check("the guest's decision matches the cards: Ferry Landing Reading Room, 14:30, was 10:00, only the time changed",
      dec["decision"]["where"] == "Ferry Landing Reading Room" and dec["decision"]["when"].endswith("14:30") and dec["decision"]["was_when"].endswith("10:00") and dec["decision"]["changed_fields"] == ["When"])
check("the guest's raw CIDs derived with the standard library match the manifests (book, both cards)", dec["book"]["cid"] == book1 and dec["editions"]["1"]["card_cid"] == card1 and dec["editions"]["2"]["card_cid"] == card2)
check("the guest read Store B from inside Computer: 5 blocks, same CIDs", {a["cid"] for a in json.load(open(J("guest-b/out/store-b-arrivals.json")))} == ids1 | ids2)
exits = dict(l.split() for l in open(J("reports/guest-exits.txt")).read().splitlines())
check("every write in a read-only guest was refused (append, remove, mkdir: nonzero exit)", all(exits[p] != "0" for p in ("probe-append", "probe-remove", "probe-mkdir")), {p: exits[p] for p in exits if p.startswith("probe")})
check("…and the restored editions are still byte-identical afterwards", all(rd("restored", f"edition-{n}", k) == v for n, files in ((1, e1), (2, e2)) for k, v in files.items()))
check("…and Store B still holds the same 5 blocks afterwards (a DELETE through the read-only mount did nothing)", len(stored) == 5 and db.execute("SELECT count(*) FROM blocks").fetchone()[0] == 5, f"sqlite probe exit {exits['probe-sqlite']}")

sent_sizes = {c: len(d) for c, d in dict(b1 + b2 + bs).items()}
numbers = {
    "book": {"bytes": len(book), "sha256": BOOK_SHA, "cid": book1, "commit": COMMIT},
    "roots": {"1": r1[0], "2": r2[0]},
    "cards": {"1": card1, "2": card2},
    "blocks": {c: {"bytes": sent_sizes[c], "codec": "dag-cbor" if libipld.decode_cid(c)["codec"] == 0x71 else "raw"} for c in sent_sizes},
    "cars": {"edition-1.car": sz("boat/edition-1.car"), "edition-2.delta.car": sz("boat/edition-2.delta.car"), "edition-2.standalone.car": sz("measure/edition-2.standalone.car")},
    "car_sha256": {n: sha(rd(p)) for n, p in (("edition-1.car", "boat/edition-1.car"), ("edition-2.delta.car", "boat/edition-2.delta.car"), ("edition-2.standalone.car", "measure/edition-2.standalone.car"))},
    "saved_bytes": saved,
    "blocks_sent": len(b1) + len(b2), "blocks_reused": 1, "blocks_changed": 2, "blocks_unique": len(ids1 | ids2),
    "manifest_sizes": {"1": sent_sizes[r1[0]], "2": sent_sizes[r2[0]]},
}
out = {"run": os.path.basename(run), "pass": all(c["pass"] for c in checks), "passed": sum(c["pass"] for c in checks), "total": len(checks), "checks": checks, "observations": obs, "numbers": numbers}
json.dump(out, open(J("checks.json"), "w"), indent=2)
open(J("checks.json"), "a").write("\n")
for c in checks:
    print(("PASS " if c["pass"] else "FAIL ") + c["title"] + ("" if c["pass"] or not c["detail"] else f"   [{c['detail']}]"))
print(f"\n{out['passed']}/{out['total']} checks passed")
sys.exit(0 if out["pass"] else 1)
