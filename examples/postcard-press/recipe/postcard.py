"""Compose an A6 postcard from a submitted order and a museum's original record."""

import base64
import hashlib
import html
import json
from pathlib import Path
import textwrap

order = json.loads(Path("/desk/order.json").read_text())
museum = order["museum"]
inscription = order["inscription"]
if (
    museum not in ("getty", "cleveland")
    or not isinstance(inscription, str)
    or not 1 <= len(inscription) <= 48
):
    raise ValueError("Choose a collection and an inscription of 1 to 48 characters.")
if any(not char.isprintable() for char in inscription):
    raise ValueError("Use a single line of printable text.")
collection = Path("/collection") / museum
record = json.loads((collection / "record.json").read_text())
if museum == "cleveland":
    data = record["data"]
    title = data["title"]
    accession = data["accession_number"]
    credit = "The Cleveland Museum of Art"
else:
    title = record["label"]["en"][0]
    accession = next(
        f["value"]["en"][0]
        for f in record["metadata"]
        if f["label"]["en"] == ["Accession Number"]
    )
    credit = "The J. Paul Getty Museum"
picture = Path("/desk") / (museum + ".jpg")
pixels = picture.read_bytes()
jpeg = "\n".join(textwrap.wrap(base64.b64encode(pixels).decode(), 76))
svg = f"""<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="148mm" height="105mm" viewBox="0 0 888 630">
<title>{html.escape(title)} — museum postcard</title>
<desc>{html.escape(credit)}, {html.escape(accession)}. Vincent van Gogh. {html.escape(inscription)}</desc>
<rect width="888" height="630" fill="#f5f0df"/>
<image x="34" y="28" width="820" height="421" preserveAspectRatio="xMidYMid slice" xlink:href="data:image/jpeg;base64,{jpeg}"/>
<text x="34" y="480" fill="#53685b" font-family="sans-serif" font-size="14" letter-spacing="1.5">VINCENT VAN GOGH  /  SAINT-RÉMY</text>
<text x="34" y="517" fill="#243a33" font-family="serif" font-size="29">{html.escape(title)}</text>
<text x="34" y="545" fill="#45584d" font-family="sans-serif" font-size="16">{html.escape(credit)} · {html.escape(accession)}</text>
<line x1="34" x2="854" y1="562" y2="562" stroke="#879b88"/>
<text x="34" y="598" fill="#784637" font-family="serif" font-style="italic" font-size="24">{html.escape(inscription)}</text>
</svg>"""
path = Path("/desk/postcard.svg")
# The read tool returns text lines. Avoid a terminal newline so its block is the complete SVG.
path.write_text(svg)
receipt = {
    "museum": museum,
    "title": title,
    "inscription": inscription,
    "credit": credit,
    "accession": accession,
    "source_jpeg_sha256": hashlib.sha256(
        (collection / "painting.jpg").read_bytes()
    ).hexdigest(),
    "thumbnail_sha256": hashlib.sha256(pixels).hexdigest(),
    "svg_bytes": len(path.read_bytes()),
    "svg_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
}
Path("/desk/receipt.json").write_text(json.dumps(receipt, ensure_ascii=False))
print(json.dumps(receipt, ensure_ascii=False))
