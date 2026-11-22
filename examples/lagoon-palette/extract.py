"""Recover the three original PNGs from the verified Venice reader."""
import argparse
import base64
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path


class Capture(HTMLParser):
    def __init__(self):
        super().__init__()
        self.inside = False
        self.parts = []

    def handle_starttag(self, tag, attrs):
        if tag == "script":
            self.inside = dict(attrs).get("id") == "capture"

    def handle_data(self, data):
        if self.inside:
            self.parts.append(data)

    def handle_endtag(self, tag):
        if tag == "script":
            self.inside = False


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("output", type=Path)
args = parser.parse_args()
source = Path(__file__).resolve().parent.parent / "lagoon-windows" / "sample"
capture = Capture()
capture.feed((source / "lagoon.html").read_text())
windows = json.loads("".join(capture.parts))["windows"]
hashes = {w["id"]: w["png_sha256"] for w in json.loads((source / "windows.json").read_text())["windows"]}
assert set(hashes) == {"san-marco", "burano", "lido"}
images = {w["id"]: base64.b64decode(w["png"], validate=True) for w in windows}
assert {name: hashlib.sha256(data).hexdigest() for name, data in images.items()} == hashes
args.output.mkdir(parents=True, exist_ok=False)
for name, data in images.items():
    (args.output / (name + ".png")).write_bytes(data)
