"""Replay cohort-3 field cases from cohort3/manifest.json and evaluate their predicates.

Each case runs once in a new temporary workspace with an empty HOME, only its declared MCP aliases and
HTTPS prefixes, and no credentials. Receipts keep the predicate results and a short allow-list of
key=value observations; they do not keep the full stdout."""
import argparse, datetime, hashlib, json, re, subprocess, tempfile, time
from pathlib import Path

HERE = Path(__file__).resolve().parent / "cohort3"
manifest = json.loads((HERE / "manifest.json").read_text())
cases = {c["id"]: c for c in manifest["cases"]}
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("case", nargs="+", help="case id(s), or 'all'")
parser.add_argument("--computer", type=Path, required=True)
parser.add_argument("--python-dir", type=Path, required=True)
parser.add_argument("--output", type=Path, required=True, help="new directory for receipts")
parser.add_argument("--pause", type=float, default=4.0, help="seconds between cases")
args = parser.parse_args()
ids = list(cases) if args.case == ["all"] else args.case
unknown = [i for i in ids if i not in cases]
if unknown:
    parser.error("unknown case: " + ", ".join(unknown))


def kv(out, key):
    m = re.search(r"^" + re.escape(key) + r"=(.*)$", out, re.M)
    return m.group(1).strip() if m else None


def number(out, key):
    try:
        return float(kv(out, key))
    except (TypeError, ValueError):
        return None


def evaluate(pred, out):
    t = pred["type"]
    if t == "contains":
        return f"output contains {pred['text']!r}", pred["text"].lower() in out.lower()
    if t == "eq":
        a, b = kv(out, pred["a"]), kv(out, pred["b"])
        return f"{pred['desc']} ({a} == {b})", a is not None and a == b
    if t == "within":
        x, y = number(out, pred["a"]), number(out, pred["b"])
        if x is None or y is None:
            return pred["desc"] + " (missing values)", False
        return f"{pred['desc']} ({x:g} vs {y:g}, tol {pred['pct']}%)", abs(x - y) <= max(x, y) * pred["pct"] / 100 + 1e-9
    if t == "flag":
        return pred["desc"], kv(out, pred["key"]) == "1"
    if t == "min":
        x = number(out, pred["key"])
        return f"{pred['desc']} (observed {x:g})" if x is not None else pred["desc"] + " (missing)", x is not None and x >= pred["value"]
    raise ValueError(t)


python = args.python_dir.resolve()
args.output.mkdir(parents=True, exist_ok=False)
failed = 0
for n, case_id in enumerate(ids):
    case = cases[case_id]
    command = [str(args.computer.resolve())]
    for alias, url in case["servers"].items():
        command.extend(["-mcp", f"{alias}={url}"])
    for url in case["allow"]:
        command.extend(["-allow-url", url])
    command.extend(["-python", str(python / "python.wasm"), "-python-lib", str(python / "lib")])
    script = (HERE / "scripts" / (case_id + ".sh")).read_text()
    with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as work:
        command.extend(["-mount", f"{work}:/work", "-workdir", "/work", "-c", "set -eo pipefail\n" + script])
        observed = datetime.datetime.now(datetime.timezone.utc).isoformat()
        result = subprocess.run(command, env={"HOME": home, "PATH": "/usr/bin:/bin", "TZ": "UTC", "LANG": "en_US.UTF-8"},
                                capture_output=True, text=True, timeout=180)
    checks = []
    for pred in case["predicates"]:
        desc, ok = evaluate(pred, result.stdout)
        checks.append({"check": desc, "passed": bool(ok)})
    passed = result.returncode == 0 and bool(checks) and all(c["passed"] for c in checks)
    receipt = {"case": case_id, "observed_utc": observed, "exit": result.returncode,
               "stdout_sha256": hashlib.sha256(result.stdout.encode()).hexdigest(),
               "observed": {k: kv(result.stdout, k) for k in case["record"]},
               "checks": checks, "passed": passed}
    (args.output / (case_id + ".json")).write_text(json.dumps(receipt, ensure_ascii=False, indent=1) + "\n")
    print(("PASS " if passed else "FAIL ") + case_id, flush=True)
    if not passed:
        failed += 1
        print(result.stderr[-400:])
    if n < len(ids) - 1:
        time.sleep(args.pause)
raise SystemExit(1 if failed else 0)
