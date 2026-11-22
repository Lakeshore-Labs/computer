"""Build the festival calendar and lobby program inside the guest."""
import datetime as dt
import json
import sys
from html import escape
from pathlib import Path

edition = sys.argv[1]
sequence = {"original": 0, "amended": 1}[edition]
items = json.loads(Path("program.json").read_text())
start = dt.datetime(2026, 11, 23, 18, tzinfo=dt.timezone.utc)
published = ("20261122T160000Z", "20261122T170000Z")[sequence]
lines = ["BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Lakeshore Labs//Short Film Matinee//EN", "CALSCALE:GREGORIAN"]
rows = []
for index, film in enumerate(items, 1):
    end = start + dt.timedelta(minutes=film["minutes"])
    lines.extend(["BEGIN:VEVENT", "UID:" + film["id"] + "@festival.example",
                  "DTSTAMP:" + published, "SEQUENCE:" + str(sequence),
                  start.strftime("DTSTART:%Y%m%dT%H%M%SZ"), end.strftime("DTEND:%Y%m%dT%H%M%SZ"),
                  "SUMMARY:" + film["title"], "LOCATION:Screen One", "END:VEVENT"])
    rows.append(f'<li><span class="time">{start:%H:%M}</span><div><h2>{escape(film["title"])}</h2>'
                f'<p>{film["minutes"]} minutes &nbsp; / &nbsp; ends {end:%H:%M}</p></div></li>')
    start = end + dt.timedelta(minutes=5)
lines.append("END:VCALENDAR")
Path(edition + ".ics").write_bytes(("\r\n".join(lines) + "\r\n").encode())
template = Path("/recipe/lobby.html").read_text()
page = template.replace("{{edition}}", edition).replace("{{rows}}", "".join(rows))
Path(edition + ".html").write_text(page)
print(f"Published {edition} lobby program and calendar for {len(items)} films.")
