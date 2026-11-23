"""Build one disposable store set under RUN: a synthetic PostgreSQL cluster (UNIX socket only) and a
DuckDB file whose view reads a real Parquet file. Usage: prepare.py RUN FIXTURE_DIR (run with a Python that has duckdb)."""
import os, random, subprocess, sys
from pathlib import Path
import duckdb

run, fixture = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve()
PGBIN = os.environ.get("PGBIN", "/opt/homebrew/bin")
sock = run / "s"; data = run / "pgdata"
sock.mkdir(parents=True, exist_ok=True)
port = random.randint(20000, 60000)
env = {**os.environ, "PGHOST": str(sock), "PGPORT": str(port), "PGUSER": "canteen_owner"}
def pg(*a, **k): return subprocess.run([f"{PGBIN}/{a[0]}", *a[1:]], env=env, check=True, capture_output=True, text=True, **k)

pg("initdb", "-D", str(data), "-U", "canteen_owner", "-A", "trust", "-E", "UTF8", "--no-locale")
pg("pg_ctl", "-D", str(data), "-w", "-l", str(run / "postgres.log"), "-o",
   f"-c listen_addresses= -c unix_socket_directories={sock} -c port={port}", "start")
pg("psql", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "CREATE DATABASE canteen")
sql = f"""
CREATE TABLE orders(hall text NOT NULL, service text NOT NULL, meal text NOT NULL, ordered integer NOT NULL);
\\copy orders FROM '{fixture / 'orders.csv'}' WITH (FORMAT csv, HEADER true)
CREATE VIEW order_totals AS SELECT hall, service, meal, SUM(ordered)::int AS ordered FROM orders GROUP BY hall, service, meal;
CREATE ROLE canteen_reader LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO canteen_reader;
GRANT SELECT ON order_totals TO canteen_reader;
"""
subprocess.run([f"{PGBIN}/psql", "-d", "canteen", "-v", "ON_ERROR_STOP=1", "-q"], env=env, input=sql, check=True, text=True, capture_output=True)

parquet = run / "loaded.parquet"
con = duckdb.connect()
con.execute(f"COPY (SELECT hall, service, meal, loaded::INTEGER AS loaded FROM read_csv('{fixture / 'loaded.csv'}', header=true)) "
            f"TO '{parquet}' (FORMAT parquet, COMPRESSION zstd)")
con.close()
db = duckdb.connect(str(run / "loads.duckdb"))
db.execute(f"CREATE VIEW loaded_totals AS SELECT hall, service, meal, SUM(loaded)::INTEGER AS loaded "
           f"FROM read_parquet('{parquet}') GROUP BY hall, service, meal")
db.close()
(run / "pg.env").write_text(f"PGHOST={sock}\nPGPORT={port}\n")
print(port)
