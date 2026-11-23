"""Open the tour pack, check its three proofs and build a portable visitor reader."""

import base64
import hashlib
import json
from pathlib import Path
import sys
import tarfile
import xml.etree.ElementTree as ET

source, destination = map(Path, sys.argv[1:])
with tarfile.open(source, "r:gz") as archive:
    receipt = json.loads(archive.extractfile("operator-receipt.json").read())
    cards = []
    for card in receipt["cards"]:
        name = "cards/" + card["file"]
        svg = archive.extractfile(name).read()
        png = archive.extractfile(name.removesuffix(".svg") + ".png").read()
        assert hashlib.sha256(svg).hexdigest() == card["svg_sha256"]
        assert hashlib.sha256(png).hexdigest() == card["png_sha256"]
        labels = [t.text for t in ET.fromstring(svg).findall("{*}text")]
        assert card["inscription"] in labels
        cards.append({**card, "data": base64.b64encode(svg).decode()})
    assert len(cards) == 3 and len({c["svg_sha256"] for c in cards}) == 3
template = Path(__file__).with_name("reader.html").read_text()
destination.write_text(
    template.replace(
        "{{CARDS}}", json.dumps(cards, ensure_ascii=False).replace("<", r"\u003c")
    )
)
