"""Replay one fixed public fieldwork recipe with an empty home and workspace."""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tempfile

RECIPES = Path(__file__).resolve().parent / "recipes"
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("case", choices=sorted(p.stem for p in RECIPES.glob("*.json")))
parser.add_argument("--computer", type=Path, required=True)
parser.add_argument("--python-dir", type=Path, required=True)
parser.add_argument("--output", type=Path, required=True)
args = parser.parse_args()
spec = json.loads((RECIPES / (args.case + ".json")).read_text())
args.output.mkdir(parents=True, exist_ok=False)
command = [str(args.computer.resolve())]
for alias, url in spec["servers"].items():
    command.extend(["-mcp", f"{alias}={url}"])
for url in spec["allow"]:
    command.extend(["-allow-url", url])
python = args.python_dir.resolve()
command.extend(["-python", str(python / "python.wasm"), "-python-lib", str(python / "lib")])
script = (RECIPES / (args.case + ".sh")).read_text()
with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as work:
    command.extend(["-mount", f"{work}:/work", "-workdir", "/work", "-c", "set -eo pipefail\n" + script])
    observed = datetime.datetime.now(datetime.timezone.utc).isoformat()
    result = subprocess.run(command, env={"HOME": home, "PATH": "/usr/bin:/bin", "TZ": "UTC", "LANG": "en_US.UTF-8"},
                            capture_output=True, text=True, timeout=150)
checks = {
    "still-met-van-gogh": [r"fields matching Met API: 6 of 6$", r"status: ok$"],
    "story-holbein-basel-trip": [r"= Kunstmuseum Basel", r"city is Basel in both sources: True$",
                                 r"year within 2 of Wikidata: True$", r"min equals stated \d+ min: True$"],
    "story-warsaw-sky-night": [r"CHECK sunset agrees within 5 min: True$", r"CHECK dark window starts after civil twilight: True$",
                               r"CHECK kosmos moon within 6 points of model: True$", r"CHECK Geminids peak is Dec 13/14: True$"],
}[args.case]
observations = {check: bool(re.search(check, result.stdout, re.MULTILINE)) for check in checks}
passed = result.returncode == 0 and all(observations.values())
receipt = {"case": args.case, "observed_utc": observed, "exit": result.returncode,
           "stdout_sha256": hashlib.sha256(result.stdout.encode()).hexdigest(), "checks": observations, "passed": passed}
(args.output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
(args.output / "transcript.txt").write_text(result.stdout)
print(json.dumps(receipt, indent=2))
if result.returncode:
    print(result.stderr[-800:])
raise SystemExit(0 if passed else 1)
