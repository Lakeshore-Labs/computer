"""Draw the four-frame ferry storyboard from a finished report. Run on the host:

    python3 examples/ferry/storyboard.py --report ./bin/ferry-report --output ./bin/ferry-story.gif

Every number comes from input/boardings.json, and the report's totals.csv and ferry.png. The script
stops if totals.csv disagrees with the input. It needs only Python's standard library, plus the
public rsvg-convert (librsvg) and ffmpeg programs on the host. Fonts come from the host, so the pixels
can differ a little between machines.
"""
import argparse
import base64
import csv
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
from tempfile import TemporaryDirectory
from xml.sax.saxutils import escape

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
parser.add_argument("--report", required=True, type=Path, help="Directory holding totals.csv and ferry.png")
parser.add_argument("--output", required=True, type=Path, help="GIF file to write")
parser.add_argument("--overwrite", action="store_true", help="Replace the output file if it exists")
args = parser.parse_args()
for tool in ("rsvg-convert", "ffmpeg"):
    if not shutil.which(tool):
        sys.exit(f"error: {tool} is not on PATH")
if args.output.suffix != ".gif":
    sys.exit("error: --output must end in .gif")
if args.output.exists() and not args.overwrite:
    sys.exit(f"error: {args.output} exists. Pass --overwrite to replace it.")

source = (here / "input/boardings.json").read_bytes()
records = json.loads(source)
digest = hashlib.sha256(source).hexdigest()[:12]
routes = list(dict.fromkeys(r["route"] for r in records))
slots = sorted({r["slot"] for r in records})
riders = {(r["route"], r["slot"]): r["riders"] for r in records}
sums = {route: sum(riders[route, s] for s in slots) for route in routes}
csv_text = (args.report / "totals.csv").read_text(encoding="utf-8")
found = {row["route"]: int(row["boardings"]) for row in csv.DictReader(csv_text.splitlines())}
if found != sums:
    sys.exit(f"error: totals.csv {found} does not match the input {sums}")
chart = base64.b64encode((args.report / "ferry.png").read_bytes()).decode()
color = {"Pier": "#28695F", "Museum": "#BD573B", "North": "#5E628D"}  # the dispatch packet's route colors
W, H = 1000, 620

def frame(number, title, subtitle, body):
    return f"""<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="{W}" height="{H}" viewBox="0 0 {W} {H}">
<rect width="{W}" height="{H}" fill="#f9f6e9"/><rect width="{W}" height="96" fill="#1b403a"/>
<text x="40" y="44" font-family="serif" font-size="34" fill="#fffcea">{escape(title)}</text>
<text x="40" y="76" font-family="sans-serif" font-size="16" fill="#c3ddc3">{escape(subtitle)}</text>
{body}
<text x="40" y="{H - 18}" font-family="sans-serif" font-size="13" fill="#4d5f5b">Frame {number}/4 · Synthetic Harborside data · input/boardings.json sha256 {digest}… · drawn by examples/ferry/storyboard.py from this run's output</text>
</svg>"""

grid = []
grid += [f'<text x="{190 + i * 95}" y="150" font-family="sans-serif" font-size="15" fill="#4d5f5b">{s}</text>' for i, s in enumerate(slots)]
for r, route in enumerate(routes):
    y = 200 + r * 90
    grid.append(f'<rect x="40" y="{y - 30}" width="8" height="44" fill="{color[route]}"/><text x="60" y="{y}" font-family="sans-serif" font-size="22" fill="#1b403a">{route}</text>')
    grid += [f'<text x="{190 + i * 95}" y="{y}" font-family="monospace" font-size="24" fill="#1b403a">{riders[route, s]}</text>' for i, s in enumerate(slots)]
grid.append('<text x="40" y="490" font-family="sans-serif" font-size="18" fill="#1b403a">Each cell is the riders field of one departure record, as the Filesystem MCP tool serves it.</text>')

adds = []
for r, route in enumerate(routes):
    y = 190 + r * 100
    terms = " + ".join(str(riders[route, s]) for s in slots)
    adds.append(f'<text x="40" y="{y}" font-family="sans-serif" font-size="24" fill="{color[route]}" font-weight="bold">{route}</text>'
                f'<text x="40" y="{y + 32}" font-family="monospace" font-size="19" fill="#1b403a">{terms} = {sums[route]}</text>')
adds.append('<text x="40" y="520" font-family="sans-serif" font-size="18" fill="#1b403a">SQLite groups the departures by route. The host checks its totals against the original records.</text>')

lines = csv_text.strip().splitlines()
table = "".join(f'<text x="80" y="{190 + i * 44}" font-family="monospace" font-size="30" fill="#1b403a">{escape(line)}</text>' for i, line in enumerate(lines))
table += '<text x="40" y="420" font-family="sans-serif" font-size="18" fill="#1b403a">totals.csv, copied unchanged from the output directory. This file is the chart tool\'s input.</text>'

picture = f'<image x="150" y="106" width="700" height="437.5" xlink:href="data:image/png;base64,{chart}"/>'
frames = [
    frame(1, "24 departures on 3 routes", "Input: riders per departure", "".join(grid)),
    frame(2, "Add up each route", "Measured sums of the same records", "".join(adds)),
    frame(3, "One total per route", "totals.csv: " + ", ".join(f"{k} {v}" for k, v in found.items()), table),
    frame(4, f"{max(sums, key=sums.get)} carried the most boardings", "Actual ferry.png from Computer, with a zero baseline", picture),
]
args.output.parent.mkdir(parents=True, exist_ok=True)
with TemporaryDirectory() as temp:
    for i, svg in enumerate(frames, start=1):
        svg_path = Path(temp) / f"frame{i}.svg"
        svg_path.write_text(svg, encoding="utf-8")
        subprocess.run(["rsvg-convert", "-w", "800", str(svg_path), "-o", str(Path(temp) / f"frame{i}.png")], check=True)
    subprocess.run(["ffmpeg", "-v", "error", "-y", "-framerate", "1/3", "-i", str(Path(temp) / "frame%d.png"),
                    "-vf", "split[a][b];[a]palettegen=max_colors=64[p];[b][p]paletteuse", "-loop", "0", str(args.output)], check=True)
print(f"Wrote {args.output} ({args.output.stat().st_size} bytes) from {len(records)} records.")
