set -eo pipefail
python3 - <<'PY'
import csv, io, sqlite3, shutil
from tools import reference, source
payload = source.read_text_file(path="trips.csv")["content"]
rows = list(csv.DictReader(io.StringIO(payload)))
icon = reference.get_tiny_image().splitlines()[-1]
shutil.copyfile(icon, "source-logo.png")
with sqlite3.connect("ridership.db") as db:
    db.execute("DROP TABLE IF EXISTS trips")
    db.execute("CREATE TABLE trips(route TEXT, riders INTEGER)")
    db.executemany("INSERT INTO trips VALUES (?, ?)",
                   ((row["route"], int(row["riders"])) for row in rows))
print(f"Upstream input: {len(payload.encode())} bytes, {len(rows)} trips")
PY
sqlite3 -readonly -header -csv ridership.db 'SELECT route,SUM(riders) AS boardings FROM trips GROUP BY route ORDER BY boardings DESC' > totals.csv
chart bar --x route --y boardings --title 'Boardings by route' totals.csv --out report.png
cat totals.csv
identify report.png source-logo.png
