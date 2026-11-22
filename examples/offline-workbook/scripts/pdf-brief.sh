set -eo pipefail
python3 - <<'PY'
import json
from pathlib import Path
from fpdf import FPDF
from pypdf import PdfReader

rows = json.loads(Path("/source/boardings.json").read_text())
doc = FPDF()
for title, subtitle in [
    ("Harborside", "Daily ferry report"),
    ("Route detail", "Passenger boardings"),
]:
    doc.add_page()
    doc.set_fill_color(20, 73, 68)
    doc.rect(0, 0, 210, 66, style="F")
    doc.set_text_color(255, 251, 232)
    doc.set_font("Helvetica", "B", 30)
    doc.text(18, 33, title)
    doc.set_font("Helvetica", size=16)
    doc.text(18, 49, subtitle)
    doc.set_text_color(20, 73, 68)
    for i, route in enumerate(["Pier", "Museum", "North"]):
        n = sum(r["riders"] for r in rows if r["route"] == route)
        y = 94 + i * 31
        doc.set_font("Helvetica", "B", 16)
        doc.text(18, y, route)
        doc.set_fill_color(69, 141, 129)
        doc.rect(63, y - 5, n / 3, 7, style="F")
        doc.text(171, y, str(n))
    doc.set_font("Helvetica", size=12)
    doc.text(18, 220, "24 departures   /   719 boardings   /   synthetic timetable")
doc.output("operations.pdf")
r = PdfReader("operations.pdf")
print(len(r.pages), "pages ·", r.pages[0].extract_text().splitlines()[0])
PY
pdftoppm -png -f 1 -l 1 -r 95 operations.pdf operations
identify operations-1.png
