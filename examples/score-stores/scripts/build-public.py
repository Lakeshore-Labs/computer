#!/usr/bin/env python3 -I
"""Assemble the portable public bundle from run-lab.py evidence.
python3 -I build-public.py --evidence OUT --providers rclone-providers.json --computer PATH --stage STAGE"""

import argparse, base64, json, shutil, subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ap = argparse.ArgumentParser()
ap.add_argument("--evidence", required=True)
ap.add_argument("--computer", required=True)
ap.add_argument("--stage", required=True)
a = ap.parse_args()
ev_dir, stage = Path(a.evidence), Path(a.stage)
ev = json.loads((ev_dir / "evidence.json").read_text())
# Capture names and descriptions from this machine without persisting personal defaults.
prov = json.loads(
    subprocess.check_output(["rclone", "--config", "/dev/null", "config", "providers"])
)
tested = {b["rclone_type"]: b["id"] for b in ev["backends"]}
ev["inventory"] = [
    {
        "name": p["Name"],
        "description": p["Description"],
        "prefix": p["Prefix"],
        "options": len(p["Options"]),
        "status": ("tested" if p["Name"] in tested else "metadata-only"),
        "backend_id": tested.get(p["Name"]),
    }
    for p in prov
]
assert len({i["name"] for i in ev["inventory"]}) == len(ev["inventory"])
assert sum(i["status"] == "tested" for i in ev["inventory"]) == len(ev["backends"])
ev["inventory_matches_live"] = True
info = subprocess.run(
    ["go", "version", "-m", a.computer], capture_output=True, text=True
).stdout
rev = next(
    (l.split("=", 1)[1] for l in info.splitlines() if "vcs.revision=" in l), "unknown"
)
mod = next(
    (l.split("=", 1)[1] for l in info.splitlines() if "vcs.modified=" in l), "unknown"
)
import hashlib

ev["computer"] = {
    "revision": rev,
    "vcs_modified": mod,
    "sha256": hashlib.sha256(Path(a.computer).read_bytes()).hexdigest(),
    "note": "pre-built stock CLI used as-is; its source tree was not rebuilt by this lab",
}
ev["source"] = {
    "repo": "Lakeshore-Labs/computer",
    "commit": "f651ff7bca13be693bec07df48e294338ee89e63",
    "path": "examples/rehearsal-room/sample/{flute.mid,flute.musicxml}",
    "files": {
        f: {"bytes": b["source_bytes"], "sha256": b["source_sha256"]}
        for f, b in zip(["flute.mid", "flute.musicxml"], ev["backends"][0]["files"])
    },
}
files = {
    b["id"]: {
        f: base64.b64encode((ev_dir / "retrieved" / b["id"] / f).read_bytes()).decode()
        for f in ("flute.mid", "flute.musicxml")
    }
    for b in ev["backends"]
}
stage.mkdir(parents=True, exist_ok=False)
(stage / "evidence.json").write_text(json.dumps(ev, indent=1))
(stage / "evidence.js").write_text(
    "window.LAB=" + json.dumps({"evidence": ev, "files": files}) + ";\n"
)
for d in ("raw", "retrieved"):
    shutil.copytree(ev_dir / d, stage / d)
reader = HERE / "reader" if (HERE / "reader").exists() else HERE.parent / "reader"
shutil.copy(reader / "index.html", stage / "index.html")

print(
    "staged",
    stage,
    "backends",
    len(ev["backends"]),
    "inventory_matches_live",
    ev["inventory_matches_live"],
)
