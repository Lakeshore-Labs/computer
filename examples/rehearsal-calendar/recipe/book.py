"""Run in Computer. Book the first matching rehearsal slot on three local calendars."""

import datetime as dt
import json
from pathlib import Path
from tools import calendar

start = dt.datetime.fromisoformat("2026-11-29T09:00:00+00:00")
end = start + dt.timedelta(hours=4)
# The generated binding maps the Python keyword 'from' to 'x_from'.
window = {"x_from": start.isoformat(), "to": end.isoformat()}
resources = ["room", "flute", "clarinet"]
ranges = calendar.get_free_busy(calendars=resources, **window)
if isinstance(ranges, str):
    ranges = json.loads(ranges)
Path("availability.json").write_text(json.dumps(ranges, indent=2))
if "busy" not in ranges:
    raise ValueError(
        "The calendar returned no free-busy response: " + str(ranges)[:400]
    )
busy = [
    (dt.datetime.fromisoformat(r["start"]), dt.datetime.fromisoformat(r["end"]))
    for r in ranges["busy"]
]
duration = dt.timedelta(minutes=45)
slot = start
while slot + duration <= end and any(slot < b and slot + duration > a for a, b in busy):
    slot += dt.timedelta(minutes=15)
if slot + duration > end:
    raise ValueError("There is no common 45-minute slot in the requested window.")
created = calendar.create_event(
    calendar_id="room",
    summary="Last Boat Home rehearsal",
    start=slot.isoformat(),
    end=(slot + duration).isoformat(),
    location="Juniper Studio",
    description="Flute and B-flat clarinet. Bring both printed parts.",
)
if isinstance(created, str):
    created = json.loads(created)
if not created.get("created"):
    raise ValueError("The booking was not acknowledged: " + str(created)[:400])
result = {
    "start": slot.isoformat(),
    "end": (slot + duration).isoformat(),
    "subject": "Last Boat Home rehearsal",
    "room": "Juniper Studio",
    "uid": created["uid"],
    "calendar": created["calendar"],
    "availability_method": ranges["method"],
    "busy_intervals": len(busy),
}
Path("booking.json").write_text(json.dumps(result, indent=2))
print(result["subject"], result["start"], result["end"])
