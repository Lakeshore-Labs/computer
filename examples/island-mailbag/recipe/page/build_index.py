"""build_index.py RUN_DIR OUT.html: fill reader.template.html with data read from a replay's run directory.

The page is one file: CSS, JavaScript and data inline, no web fonts, no network requests. Its links are relative to the
published folder (car/, receipts/, README.md, SOURCES.md), so OUT belongs at that folder's top level. Refuses a run
whose checks did not all pass. The output depends only on the run's contents, so a faithful replay rebuilds the
published index.html byte for byte.
"""
import json, os, sys

here = os.path.dirname(os.path.abspath(__file__))
run, out_path = os.path.abspath(sys.argv[1]), sys.argv[2]
J = lambda *p: json.load(open(os.path.join(run, *p)))

checks = J("checks.json")
dec = J("guest-b/out/decision.json")
s1, s2 = J("reports/send-1.json"), J("reports/send-2.json")
dump = J("reports/store-b-dump.json")
n = checks["numbers"]
if not checks["pass"]:
    sys.exit("refusing to build the reader from a run whose checks did not all pass")

role_of = {}
for rep, ed in ((s1, 1), (s2, 2)):
    for b in rep["blocks"]:
        role_of[b["cid"]] = {"role": b["role"], "edition": ed if b["role"] != "book" else 0}
blocks = []
for cid, info in n["blocks"].items():
    r = role_of[cid]
    arrivals = [a for a in dump["arrivals"] if a["cid"] == cid]
    blocks.append({"cid": cid, "codec": info["codec"], "bytes": info["bytes"], "role": r["role"], "edition": r["edition"],
                   "arrived_in": arrivals[0]["car"] if arrivals else None, "arrival_seq": arrivals[0]["seq"] if arrivals else None})
data = {
    "base": "",
    "decision": dec["decision"],
    "book": {**dec["book"], "commit": n["book"]["commit"]},
    "editions": {k: {"root": v["root"], "card_cid": v["card_cid"], "card_bytes": v["card_bytes"], "card_sha256": v["card_sha256"], "card_text": v["card_text"]} for k, v in dec["editions"].items()},
    "blocks": blocks,
    "cars": {"one": {"file": "boat/edition-1.car", "bytes": n["cars"]["edition-1.car"], "sha256": n["car_sha256"]["edition-1.car"], "blocks": len(s1["sent"])},
             "delta": {"file": "boat/edition-2.delta.car", "bytes": n["cars"]["edition-2.delta.car"], "sha256": n["car_sha256"]["edition-2.delta.car"], "blocks": len(s2["sent"])},
             "standalone": {"file": "measure/edition-2.standalone.car", "bytes": n["cars"]["edition-2.standalone.car"], "sha256": n["car_sha256"]["edition-2.standalone.car"], "blocks": s2["standalone"]["blocks"]}},
    "saved_bytes": n["saved_bytes"],
    "counts": {"sent": n["blocks_sent"], "reused": n["blocks_reused"], "changed": n["blocks_changed"], "unique": n["blocks_unique"]},
    "checks": {"passed": checks["passed"], "total": checks["total"], "list": [{"t": c["title"], "p": c["pass"]} for c in checks["checks"]],
               "guest": {"passed": sum(c["pass"] for c in dec["checks"]), "total": len(dec["checks"])}},
    "observations": checks["observations"],
}
tpl = open(os.path.join(here, "reader.template.html")).read()
blob = json.dumps(data, ensure_ascii=False, separators=(",", ":")).replace("</", "<\\/")
html = tpl.replace("/*__DATA__*/null", blob)
assert html != tpl
open(out_path, "w").write(html)
print(f"wrote {out_path}: {len(html):,} bytes from {os.path.basename(run)}")
