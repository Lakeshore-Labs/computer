set -eo pipefail
python3 - <<'PY'
from zoneinfo import ZoneInfo
from dateutil.parser import isoparse
from tabulate import tabulate

t = isoparse("2026-07-04T15:00:00Z")
print(
    tabulate(
        [
            [z, t.astimezone(ZoneInfo(z)).strftime("%H:%M %Z")]
            for z in ("America/Los_Angeles", "Europe/London", "Asia/Tokyo")
        ],
        headers=["Port office", "Clock"],
    )
)
PY
