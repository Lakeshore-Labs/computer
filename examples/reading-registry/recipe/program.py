"""Render the reader's program from the calendar and e-book actually pulled from OCI."""

import base64
import html
import json
from pathlib import Path
from zipfile import ZipFile


def uri(mime, data):
    return f"data:{mime};base64," + base64.b64encode(data).decode()


root = Path("/deliveries")
receipt = json.loads((root / "checks.json").read_bytes())
cards = []
for r in receipt["editions"]:
    d = root / (r["edition"] + "-download")
    cal = (d / "reading-room.ics").read_bytes()
    fields = dict(row.split(":", 1) for row in cal.decode().splitlines() if ":" in row)
    time = fields["DTSTART"][9:11] + ":" + fields["DTSTART"][11:13]
    cards.append(
        {
            "edition": r["edition"],
            "time": time,
            "sequence": fields["SEQUENCE"],
            "calendar": uri("text/calendar", cal),
            "sha": r["manifest"].split(":")[1],
        }
    )
book = (root / "amended-download/four-moons.epub").read_bytes()
with ZipFile(root / "amended-download/four-moons.epub") as z:
    photo = z.read("EPUB/galilean.jpg")
template = Path("/recipe/program.html").read_text()
for k, v in [
    ("PHOTO", uri("image/jpeg", photo)),
    ("BOOK", uri("application/epub+zip", book)),
    ("EDITS", json.dumps(cards)),
    ("BOOK_DIGEST", receipt["editions"][0]["book"]),
]:
    template = template.replace("{{" + k + "}}", v)
Path("program.html").write_text(template)
