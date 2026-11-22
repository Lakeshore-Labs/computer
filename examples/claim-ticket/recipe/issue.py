"""Issue a paper claim ticket and a machine-readable PNG inside WASI Python."""

from datetime import datetime
import json
from pathlib import Path
import sys

sys.path.insert(0, "/vendor/segno-1.6.6-py3-none-any.whl")
import segno
from fpdf import FPDF

item = json.loads(Path("/recipe/lost-property.json").read_text())
code = segno.make_qr(item["payload"], error="m")
code.save("claim.png", scale=8, border=4)
paper = FPDF(unit="mm", format=(150, 85))
paper.set_creation_date(datetime.fromisoformat(item["issued_at"]))
paper.add_page()
paper.set_fill_color(246, 240, 218)
paper.rect(0, 0, 150, 85, "F")
paper.set_fill_color(41, 55, 70)
paper.rect(0, 0, 150, 16, "F")
paper.set_text_color(250, 243, 219)
paper.set_font("Helvetica", "B", 11)
paper.text(7, 10, "CITY READING ROOMS  /  LOST PROPERTY")
paper.set_text_color(40, 48, 55)
paper.set_font("Times", "", 26)
paper.text(7, 31, item["item"])
paper.set_font("Helvetica", "", 12)
paper.text(7, 41, "Pickup: " + item["desk"])
paper.set_font("Helvetica", "", 10)
paper.text(7, 50, item["collection_day"])
paper.set_font("Courier", "B", 19)
paper.text(7, 69, item["reference"])
paper.set_font("Helvetica", "", 8)
paper.text(7, 77, "Fictional collection ticket")
module = 1.05
matrix = list(code.matrix_iter(border=4))
width = len(matrix) * module
left = 143 - width
top = 23
paper.set_fill_color(255, 255, 255)
paper.rect(left, top, width, width, "F")
paper.set_fill_color(0, 0, 0)
for y, row in enumerate(matrix):
    for x, on in enumerate(row):
        if on:
            paper.rect(
                left + x * module, top + y * module, module + 0.01, module + 0.01, "F"
            )
paper.output("claim.pdf")
print(
    f"{item['reference']} · {code.designator} · {len(matrix)} modules including quiet zone"
)
