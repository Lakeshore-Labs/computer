"""Impose four original A6 pages on a single landscape A4 sheet inside Computer."""

import hashlib
import json
from pathlib import Path

from pypdf import PdfReader, PdfWriter, Transformation

MM = 72 / 25.4
root = Path("/cards")
out = Path("/work")
order = json.loads((root / "order.json").read_text())
slots = order["slots"]
if len(slots) != 4 or any(name not in ("getty", "cleveland") for name in slots):
    raise ValueError("One sheet needs four Getty or Cleveland cards.")
writer = PdfWriter()
sheet = writer.add_blank_page(width=297 * MM, height=210 * MM)
sources = {name: PdfReader(root / (name + ".pdf")).pages[0] for name in set(slots)}
for i, name in enumerate(slots):
    card = sources[name]
    if (
        abs(float(card.mediabox.width) / MM - 148) > 0.02
        or abs(float(card.mediabox.height) / MM - 105) > 0.02
    ):
        raise ValueError("The source card must have A6 dimensions.")
    # The extra millimetre in A4 landscape is split equally between the outer edges.
    x = (0.5 + 148 * (i % 2)) * MM
    y = (105 if i < 2 else 0) * MM
    sheet.merge_transformed_page(card, Transformation().translate(x, y))
writer.add_metadata(
    {"/Title": "Saint-Rémy museum postcard sheet", "/Author": "Museum print desk"}
)
with (out / "sheet.pdf").open("wb") as stream:
    writer.write(stream)
data = (out / "sheet.pdf").read_bytes()
ticket = {
    "edition": order["edition"],
    "paper_mm": [297, 210],
    "slots": slots,
    "copies": {name: slots.count(name) for name in sorted(sources)},
    "pdf_bytes": len(data),
    "pdf_sha256": hashlib.sha256(data).hexdigest(),
}
(out / "ticket.json").write_text(json.dumps(ticket, indent=2) + "\n")
print(json.dumps(ticket, ensure_ascii=False))
