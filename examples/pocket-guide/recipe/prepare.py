"""Prepare six print crops from the original public showcase images."""

import argparse
import hashlib
import json
from pathlib import Path

from PIL import Image, ImageOps

crops = [
    (0, 0, 800, 500),
    (575, 125, 1090, 595),
    (170, 565, 1300, 870),
    (50, 70, 1110, 470),
    (10, 29, 814, 275),
    (163, 230, 1115, 480),
]


def prepare(examples, output):
    output.mkdir(parents=True, exist_ok=True)
    specs = json.loads(Path(__file__).with_name("stories.json").read_text())
    hashes = []
    for story, bounds in zip(specs, crops, strict=True):
        source = examples / story["image"]
        with Image.open(source) as original:
            assert bounds[2] <= original.width and bounds[3] <= original.height
            crop = original.convert("RGB").crop(bounds)
            target = Image.new("RGB", (920, 350), "#f3f0e5")
            crop = ImageOps.contain(crop, target.size)
            target.paste(
                crop,
                ((target.width - crop.width) // 2, (target.height - crop.height) // 2),
            )
            target.save(output / f"{story['pr']}.jpg", quality=88, optimize=True)
        hashes.append(
            {
                "source": story["image"],
                "bounds": bounds,
                "sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
            }
        )
    (output / "sources.json").write_text(json.dumps(hashes, indent=2) + "\n")


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("examples", type=Path)
    ap.add_argument("output", type=Path)
    args = ap.parse_args()
    prepare(args.examples, args.output)
