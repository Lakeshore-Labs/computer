"""Independent reader of an .apkg: NO Anki code. Stdlib only (zipfile, sqlite3, compression.zstd; Python>=3.14).
usage: python3.14 -I independent_read.py FILE.apkg [OUT.json]"""

import json, os, sqlite3, sys, tempfile, zipfile
from compression import zstd

src = sys.argv[1]
with tempfile.TemporaryDirectory() as t, zipfile.ZipFile(src) as z:
    names = z.namelist()
    meta = z.read("meta") if "meta" in names else b""
    member = (
        "collection.anki21b"
        if "collection.anki21b" in names
        else "collection.anki21"
        if "collection.anki21" in names
        else "collection.anki2"
    )
    raw = z.read(member)
    if member.endswith("b"):
        raw = zstd.decompress(raw)
    p = os.path.join(t, "c.sqlite")
    open(p, "wb").write(raw)
    db = sqlite3.connect(f"file:{p}?mode=ro", uri=True)
    notes = db.execute("select id,guid,mid,flds from notes order by id").fetchall()
    cards = db.execute(
        "select id,nid,did,type,queue,due,ivl,factor,reps from cards order by id"
    ).fetchall()
    rev = db.execute(
        "select id,cid,ease,ivl,lastIvl,type from revlog order by id"
    ).fetchall()
out = {
    "zip_members": names[:6],
    "collection_member": member,
    "meta_hex": meta.hex(),
    "notes": [
        {"id": a, "guid": b, "mid": c, "fields": d.split("\x1f")}
        for a, b, c, d in notes
    ],
    "cards": [
        dict(
            zip(
                ("id", "nid", "did", "type", "queue", "due", "ivl", "factor", "reps"), c
            )
        )
        for c in cards
    ],
    "revlog": [
        dict(zip(("id", "cid", "ease", "ivl", "lastIvl", "type"), r)) for r in rev
    ],
}
if len(sys.argv) > 2:
    open(sys.argv[2], "w").write(json.dumps(out, indent=1, ensure_ascii=False))
print(json.dumps({k: (len(v) if isinstance(v, list) else v) for k, v in out.items()}))
