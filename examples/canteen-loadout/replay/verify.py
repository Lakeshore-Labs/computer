"""Independent check (no MCP, no DuckDB): psql reads the PostgreSQL rows, pyarrow reads the Parquet file,
final quantities are recomputed from those and from the guest's transfers.csv. Run: python -I verify.py RUN PGBIN OUT.json"""
import csv, json, subprocess, sys
from pathlib import Path
import pyarrow.parquet as pq

run, pgbin, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
env = dict(l.split("=", 1) for l in (run / "pg.env").read_text().split())
psql = subprocess.run([f"{pgbin}/psql", "-h", env["PGHOST"], "-p", env["PGPORT"], "-U", "canteen_owner", "-d", "canteen", "-At", "-F,",
                       "-c", "SELECT hall,service,meal,SUM(ordered) FROM orders GROUP BY 1,2,3 ORDER BY 1,2,3"],
                      check=True, capture_output=True, text=True).stdout.splitlines()
ordered = {tuple(r.split(",")[:3]): int(r.split(",")[3]) for r in psql}
pfile = run / "loaded.parquet"
raw = pfile.read_bytes()
table = pq.read_table(pfile); md = pq.ParquetFile(pfile).metadata
loaded = {}
for r in table.to_pylist(): loaded[(r["hall"], r["service"], r["meal"])] = loaded.get((r["hall"], r["service"], r["meal"]), 0) + r["loaded"]
transfers = list(csv.DictReader((run / "work/transfers.csv").open()))
final = dict(loaded)
for t in transfers:
    final[(t["from_hall"], t["service"], t["meal"])] -= int(t["units"]); final[(t["to_hall"], t["service"], t["meal"])] += int(t["units"])
shortages = {k: ordered[k] - loaded[k] for k in ordered if loaded[k] < ordered[k]}
expected = [("Lunch", "Roast vegetables", "Orchard", "Quay", 5), ("Lunch", "Roast vegetables", "Hill", "Quay", 3),
            ("Lunch", "Herb pasta", "Quay", "Orchard", 3), ("Lunch", "Herb pasta", "Hill", "Orchard", 2)]
got = [(t["service"], t["meal"], t["from_hall"], t["to_hall"], int(t["units"])) for t in transfers]
checks = {
 "postgres_rows_via_psql": len(ordered) == 6 and sum(ordered.values()) == 150,
 "parquet_is_real": raw[:4] == b"PAR1" and raw[-4:] == b"PAR1" and md.num_rows == 6,
 "parquet_total_150": sum(loaded.values()) == 150,
 "transfers_match_expected": sorted(got) == sorted(expected),
 "final_equals_ordered": final == ordered,
 "no_units_short": all(final[k] >= ordered[k] for k in ordered),
}
res = {"checks": checks, "all_pass": all(checks.values()), "psql_rows": psql, "parquet": {"rows": md.num_rows, "created_by": md.created_by,
       "compression": md.row_group(0).column(0).compression, "bytes": len(raw), "schema": str(table.schema).replace("\n", "; ")},
       "original_shortages": {"|".join(k): v for k, v in shortages.items()},
       "final": {"|".join(k): v for k, v in sorted(final.items())}, "pyarrow": __import__("pyarrow").__version__}
out.write_text(json.dumps(res, indent=1)); print(json.dumps(checks)); sys.exit(0 if res["all_pass"] else 1)
