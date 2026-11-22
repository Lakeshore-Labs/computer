set -eo pipefail
cp /source/day.csv working-copy.csv
head -2 working-copy.csv
python3 - <<'PY'
from pathlib import Path

try:
    Path("/source/scratch-test.txt").write_text("demo")
except OSError as err:
    print("Write refused:", err.strerror)
else:
    raise SystemExit("unexpected write")
PY
