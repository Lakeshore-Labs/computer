set -eo pipefail
python3 - <<'PY'
import json, sqlite3
from pathlib import Path

rows = json.loads(Path("/source/boardings.json").read_text())
with sqlite3.connect("harbor.db") as db:
    db.execute("DROP TABLE IF EXISTS trips")
    db.execute("CREATE TABLE trips(route TEXT, riders INTEGER, slot TEXT)")
    db.executemany("INSERT INTO trips VALUES (:route, :riders, :slot)", rows)
print("Inserted", len(rows), "departures")
PY
sqlite3 -readonly -header -table harbor.db 'SELECT SUM(riders) AS total FROM trips'
