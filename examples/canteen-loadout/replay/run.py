#!/usr/bin/env python3
"""One fresh end-to-end run in a newly created isolated store. See README.md.
python3 run.py --support DIR --pywasi DIR --out DIR [--computer computer] [--fixture DIR]"""
import argparse, hashlib, json, os, re, shutil, subprocess, sys, tempfile
from pathlib import Path
here = Path(__file__).resolve().parent
PYARROW = os.environ.get("PYARROW", "26.0.1")
ap = argparse.ArgumentParser()
ap.add_argument("--support", type=Path, required=True)   # has uv/bin, uv/tools, npm/node_modules (see install.sh)
ap.add_argument("--pywasi", type=Path, required=True)    # dir with python.wasm and lib/
ap.add_argument("--out", type=Path, required=True)
ap.add_argument("--computer", default="computer")
ap.add_argument("--fixture", type=Path, default=here / "fixture")
ap.add_argument("--pgbin", default="/opt/homebrew/bin")
ap.add_argument("--keep", action="store_true")
a = ap.parse_args()
S, out = a.support.resolve(), a.out.resolve(); out.mkdir(parents=True, exist_ok=False)
run = Path(tempfile.mkdtemp(prefix="cnt.", dir="/tmp")).resolve()   # short path: UNIX socket paths are length-limited
(run / "work").mkdir()
dpy = S / "uv/tools/mcp-server-motherduck/bin/python"
def clean(t):
    for p, n in ((str(run), "<RUN>"), (str(S), "<SUPPORT>"), (str(a.pywasi.resolve()), "<PYWASI>"), (str(here), "<REPLAY>"), (os.path.expanduser("~"), "~"), (str(Path(run).resolve()), "<RUN>"), ("/private<RUN>", "<RUN>")):
        t = t.replace(p, n)
    return t
rec = {"steps": []}
def step(name, cmd, **kw):
    p = subprocess.run(cmd, capture_output=True, text=True, **kw)
    rec["steps"].append({"name": name, "exit": p.returncode, "stdout": clean(p.stdout), "stderr": clean(p.stderr)})
    return p
ok = False
try:
    prep = step("prepare (initdb, psql \\copy, DuckDB COPY TO parquet)", [str(dpy), str(here / "prepare.py"), str(run), str(a.fixture)], env={**os.environ, "PGBIN": a.pgbin})
    assert prep.returncode == 0, prep.stderr
    env = dict(l.split("=", 1) for l in (run / "pg.env").read_text().split())
    cfg = {"mounts": [f"{run}/work:/work", f"{here}:/scripts:ro"], "mcpServers": {
        "orders_pg": {"command": "node", "args": [str(S / "npm/node_modules/@modelcontextprotocol/server-postgres/dist/index.js"),
                      f"postgresql://canteen_reader@localhost/canteen?host={env['PGHOST']}&port={env['PGPORT']}"]},
        "loads_duck": {"command": str(S / "uv/bin/mcp-server-motherduck"), "args": ["--db-path", str(run / "loads.duckdb")]}}}
    (run / "computer.json").write_text(json.dumps(cfg))
    base = [a.computer, "-config", str(run / "computer.json"), "-python", str(a.pywasi / "python.wasm"), "-python-lib", str(a.pywasi / "lib")]
    clean_env = {k: v for k, v in os.environ.items() if not k.lower().startswith("motherduck")}
    def comp(name, script): return step(name, base + ["-c", "set -eo pipefail\n" + script], env=clean_env, timeout=300)
    comp("generated upstream commands", "orders-pg --help; loads-duck --help")
    writes_rejected = True
    for name, query, message in (
        ("PostgreSQL", "orders-pg query --sql 'DELETE FROM orders'", "cannot execute DELETE in a read-only transaction"),
        ("DuckDB", "loads-duck execute-query --sql 'CREATE TABLE x(a int)'", "attached in read-only mode"),
    ):
        probe = comp(name + " refuses writes", query)
        rejected = probe.returncode != 0 and message in (probe.stdout + probe.stderr)
        if name == "DuckDB" and probe.returncode == 0:
            try:
                payload = json.loads(probe.stdout)
            except ValueError:
                payload = None
            rejected = (isinstance(payload, dict) and payload.get("success") is False
                        and message in str(payload.get("error", "")))
        writes_rejected &= rejected
    comp("DuckDB view references the Parquet file", "loads-duck execute-query --sql \"SELECT sql FROM duckdb_views() WHERE view_name='loaded_totals'\"; loads-duck execute-query --sql \"SELECT num_rows, created_by FROM parquet_file_metadata('%s/loaded.parquet')\"" % run)
    g = comp("guest (WASI Python) retrieves both tables, joins, writes transfers", "python3 /scripts/guest.py; cat /work/transfers.csv")
    assert g.returncode == 0, g.stderr
    v = subprocess.run(["uv", "run", "--offline", "--python", "3.13", "--no-project", "--with", "pyarrow==" + PYARROW, "python", "-I", str(here / "verify.py"), str(run), a.pgbin, str(out / "independent-verify.json")], capture_output=True, text=True)
    rec["steps"].append({"name": "independent verify (psql + pyarrow)", "exit": v.returncode, "stdout": v.stdout, "stderr": clean(v.stderr)})
    for f in ("transfers.csv", "balance.json"): shutil.copy(run / "work" / f, out / f)
    shutil.copy(run / "loaded.parquet", out / "loaded.parquet")
    ok = writes_rejected and v.returncode == 0
finally:
    try:
        stop = step("stop PostgreSQL", [f"{a.pgbin}/pg_ctl", "-D", str(run / "pgdata"), "-m", "fast", "-w", "stop"])
        stopped = stop.returncode == 0
    except OSError as error:
        stopped = False
        rec["shutdown_error"] = str(error)
    if not stopped:
        ok = False
        rec["retained_directory"] = str(run)
        print(f"PostgreSQL shutdown unconfirmed. Kept {run}", file=sys.stderr)
    elif not a.keep:
        shutil.rmtree(run)
    rec["pass"] = ok
    (out / "receipt.json").write_text(json.dumps(rec, indent=1))
    print("PASS" if ok else "FAIL", out.name)
sys.exit(0 if ok else 1)
