"""Build an offline scheduling desk using only the saved calendars and agenda."""

import base64
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
checks = json.loads((out / "calendar-check.json").read_text())
payload = {
    "booking": json.loads((out / "booking.json").read_text()),
    "events": checks["verified_events"],
    "agenda": checks["native_agenda"]["agenda"],
    "ics": base64.b64encode((out / "rehearsal.ics").read_bytes()).decode(),
}
template = Path(__file__).with_name("reader.html").read_text()
(out / "studio.html").write_text(
    template.replace("__PAYLOAD__", json.dumps(payload).replace("<", "\\u003c"))
)
