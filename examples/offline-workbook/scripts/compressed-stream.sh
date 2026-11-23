set -eo pipefail
python3 - <<'PY'
import csv, gzip, io, hashlib
from pathlib import Path

raw = Path("/source/day.csv").read_bytes()
p = Path("timetable.csv.gz")
p.write_bytes(gzip.compress(raw, mtime=0))
with gzip.open(p, "rt") as f:
    print("Departure rows:", sum(1 for _ in csv.DictReader(f)))
assert gzip.decompress(p.read_bytes()) == raw
print("SHA-256:", hashlib.sha256(raw).hexdigest())
PY
