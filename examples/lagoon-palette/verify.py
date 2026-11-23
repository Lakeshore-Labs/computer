"""Verify the RGB values independently with Pillow's PNG and GIMP palette readers."""
import argparse
import hashlib
import json
from pathlib import Path
from PIL import GimpPaletteFile, Image

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("scenes", type=Path)
parser.add_argument("output", type=Path)
args = parser.parse_args()
read = lambda name: (args.output / name).read_bytes()
selected = json.loads(read("selection.json"))
native = json.loads(read("swift.json"))
assert native["native_round_trip"] and native["tools"] == ["bash", "edit", "read", "write"]
assert len(selected) == 6 and native["native_keys"] == [s["name"] for s in selected]
with (args.output / "venetian-lagoon.gpl").open("rb") as file:
    palette, mode = GimpPaletteFile.GimpPaletteFile(file).getpalette()
assert mode == "RGB"
css = read("venetian-lagoon.css").decode().splitlines()
for i, swatch in enumerate(selected):
    scene = args.scenes / (swatch["site"] + ".png")
    assert hashlib.sha256(scene.read_bytes()).hexdigest() == native["png_sha256"][swatch["site"]]
    with Image.open(scene) as image:
        pixel = image.convert("RGB").getpixel((swatch["x"], swatch["y"]))
    assert tuple(swatch["rgb"]) == pixel == tuple(palette[3*i:3*i+3])
    name = "-".join(swatch["name"].lower().split())
    assert f"  --{name}: #{bytes(pixel).hex().upper()};" in css
print("All six source PNG pixels, Swift samples, GIMP entries and CSS colors agree.")
