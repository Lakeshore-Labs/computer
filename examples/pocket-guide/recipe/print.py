"""Lay out the field guide and draw its vector QR code in Computer's WASI Python."""

from datetime import datetime, timezone
import io
import json
from pathlib import Path
import sys

from fpdf import FPDF
from pypdf import PdfReader, PdfWriter
from pypdf.generic import (
    DecodedStreamObject,
    DictionaryObject,
    NameObject,
    NumberObject,
)

sys.path.insert(0, "/vendor/segno-1.6.6-py3-none-any.whl")
import segno

base = "https://github.com/Lakeshore-Labs/computer/pull/"
stories = json.loads(Path("/recipe/stories.json").read_text())
guide_url = base + "56"
film_url = base + "62"
ink = (25, 60, 55)
paper = (250, 248, 239)
accent = (166, 72, 43)
pdf = FPDF(orientation="landscape", unit="mm", format="A4")
pdf.set_creation_date(datetime(2026, 11, 22, tzinfo=timezone.utc))
pdf.set_title("Six things made with Computer")
pdf.set_author("Computer field guide")
pdf.set_auto_page_break(False)
pdf.add_page()
pdf.set_fill_color(*paper)
pdf.rect(0, 0, 297, 210, "F")


def text(x, y, message, font="Helvetica", size=10, style="", color=ink):
    pdf.set_font(font, style, size)
    pdf.set_text_color(*color)
    pdf.text(x, y, message)


pdf.set_fill_color(*ink)
pdf.rect(0, 0, 297, 40, "F")
text(
    10,
    9,
    "LAKESHORE LABS    /    COMPUTER FIELD GUIDE",
    size=8,
    style="B",
    color=(202, 221, 188),
)
text(9, 24, "Six things made with Computer", "Times", 34, color=paper)
text(
    10,
    34,
    "Four MCP tools give a client a shell, programs and files. Follow the output into a second application.",
    size=11,
    color=paper,
)
qr = list(segno.make_qr(guide_url, error="m").matrix_iter(border=4))
side = 28
left, top, unit = 258, 5.5, side / len(qr)
pdf.set_fill_color(255, 255, 255)
pdf.rect(left, top, side, side, "F")
pdf.set_fill_color(0, 0, 0)
for y, row in enumerate(qr):
    for x, dark in enumerate(row):
        if dark:
            pdf.rect(left + x * unit, top + y * unit, unit + 0.01, unit + 0.01, "F")
pdf.link(left, top, side, side, guide_url)
for i, story in enumerate(stories):
    x, y = 10 + (i % 3) * 94, 46 + (i // 3) * 73
    # Leave room for the original JPEG stream, embedded below.
    text(x, y + 39, story["role"], size=7.6, style="B", color=accent)
    text(x, y + 48, story["title"], "Times", 22)
    for j, line in enumerate(story["lines"]):
        text(x, y + 55 + j * 5, line, size=10)
    pdf.set_draw_color(167, 181, 166)
    pdf.line(x, y + 65, x + 88, y + 65)
    pdf.link(x, y, 88, 64, base + str(story["pr"]))
text(
    10,
    197,
    "Open a panel for its runnable example. Scan the code for the offline field guide.",
    size=10,
)
text(
    10,
    204,
    "Imagery contains modified Copernicus Sentinel data (2024). Lunch, radio, library and ferry scenarios use synthetic records.",
    size=8,
)
text(230, 199, "Watch the 79-second film", size=10, style="B", color=accent)
pdf.link(229, 192, 59, 12, film_url)
# This WASI build has no Pillow. The host already supplied 920 x 350 RGB JPEGs.
# Standard PDF image objects embed their existing DCT streams without transcoding.
writer = PdfWriter(clone_from=io.BytesIO(bytes(pdf.output())))
page = writer.pages[0]
objects = DictionaryObject()
page["/Resources"][NameObject("/XObject")] = objects
commands = []
points = 72 / 25.4
for i, story in enumerate(stories):
    left, top = 10 + (i % 3) * 94, 46 + (i // 3) * 73
    name = NameObject(f"/Shot{i + 1}")
    shot = DecodedStreamObject()
    shot.set_data(Path(f"/thumbs/{story['pr']}.jpg").read_bytes())
    shot.update(
        {
            NameObject("/Type"): NameObject("/XObject"),
            NameObject("/Subtype"): NameObject("/Image"),
            NameObject("/Width"): NumberObject(920),
            NameObject("/Height"): NumberObject(350),
            NameObject("/ColorSpace"): NameObject("/DeviceRGB"),
            NameObject("/BitsPerComponent"): NumberObject(8),
            NameObject("/Filter"): NameObject("/DCTDecode"),
        }
    )
    objects[name] = writer._add_object(shot)
    commands.append(
        f"q {88 * points:.3f} 0 0 {33.5 * points:.3f} {left * points:.3f} {(210 - top - 33.5) * points:.3f} cm {name} Do Q"
    )
content = DecodedStreamObject()
content.set_data(
    page.get_contents().get_data() + ("\n" + "\n".join(commands) + "\n").encode()
)
page.replace_contents(content)
with open("pocket-guide.pdf", "wb") as out:
    writer.write(out)
reader = PdfReader("pocket-guide.pdf")
assert len(reader.pages) == 1
links = [a.get_object()["/A"]["/URI"] for a in reader.pages[0]["/Annots"]]
assert sorted(links) == sorted(
    [guide_url, film_url] + [base + str(s["pr"]) for s in stories]
)
print("Wrote one A4 landscape page, six illustrated stories and eight source links.")
