"""Attach the three captured SVG downloads to the offline museum proof sheet."""

import base64
from html.parser import HTMLParser
import json
from pathlib import Path

here = Path(__file__).resolve().parent


class Downloads(HTMLParser):
    def __init__(self):
        super().__init__()
        self.files = {}

    def handle_starttag(self, tag, attributes):
        d = dict(attributes)
        if tag == "a" and "download" in d:
            filename = d["href"]
            self.files[filename] = base64.b64encode(
                (here / "sample" / filename).read_bytes()
            ).decode()


template = (here / "proofs.html").read_text()
parser = Downloads()
parser.feed(template)
(here / "sample/proofs.html").write_text(
    template.replace("{{FILES}}", json.dumps(parser.files, separators=(",", ":")))
)
