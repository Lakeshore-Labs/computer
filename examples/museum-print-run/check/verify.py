"""Rasterize each imposed quadrant and classify it against both original card PDFs."""

import json
from pathlib import Path
import subprocess
import sys
from PIL import Image, ImageChops, ImageStat

cards, out = map(Path, sys.argv[1:])


def render(pdf, dest, width, height):
    subprocess.run(
        [
            "pdftoppm",
            "-scale-to-x",
            str(width),
            "-scale-to-y",
            str(height),
            "-singlefile",
            "-png",
            str(pdf),
            str(dest),
        ],
        check=True,
    )
    with Image.open(dest.with_suffix(".png")) as image:
        return image.convert("RGB")


sources = {
    name: render(cards / (name + ".pdf"), cards / (name + "-check"), 592, 420)
    for name in ("getty", "cleveland")
}
checks = []
for edition in ["first", "amended"]:
    ticket = json.loads((out / edition / "ticket.json").read_text())
    pdf = out / edition / "sheet.pdf"
    im = render(pdf, cards / (edition + "-check"), 1188, 840)
    texts = subprocess.check_output(["pdftotext", "-layout", str(pdf), "-"], text=True)
    assert texts.count("From the garden") == ticket["copies"]["getty"]
    assert texts.count("Wish you were here") == ticket["copies"]["cleveland"]
    for i, expected in enumerate(ticket["slots"]):
        x, y = 2 + 592 * (i % 2), 420 * (i // 2)
        panel = im.crop((x, y, x + 592, y + 420))
        delta = {
            name: sum(ImageStat.Stat(ImageChops.difference(panel, source)).mean) / 3
            for name, source in sources.items()
        }
        assert (
            min(delta, key=delta.get) == expected
            and delta[expected] < 3
            and max(delta.values()) > 20
        ), (expected, delta)
        checks.append(
            {
                "edition": edition,
                "position": i + 1,
                "expected": expected,
                "nearest_original": min(delta, key=delta.get),
                "mean_absolute_rgb_difference": delta,
            }
        )
(out / "pixel-checks.json").write_text(json.dumps(checks, indent=2) + "\n")
print(
    "Eight rendered postcard positions and both sheets' extracted inscriptions agree with their orders."
)
