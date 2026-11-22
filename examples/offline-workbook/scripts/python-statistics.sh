set -eo pipefail
python3 - <<'PY'
import json, statistics
from pathlib import Path
from collections import defaultdict

data = defaultdict(list)
for row in json.loads(Path("/source/boardings.json").read_text()):
    data[row["route"]].append(row["riders"])
for route, counts in sorted(data.items()):
    print(f"{route:<7} median {statistics.median(counts):4.1f}   peak {max(counts)}")
PY
