"""Render a replay's transcript and saved image as one compact picture. Run on the host:

    python3 examples/reference-servers/render.py --report ./bin/reference-results --output ./bin/seven-servers.png

It reads session.txt and logo.png from a finished replay and draws the transcript's text unchanged, one
card per server, with the server, Computer command and tool named in each header. The card for the
Everything server also shows the saved logo.png, enlarged. It uses Python's standard library and the public
rsvg-convert (librsvg) on the host. Fonts come from the host, so pixels can differ a little between machines.
"""
import argparse
import base64
from pathlib import Path
import re
import shutil
import subprocess
import sys
from tempfile import TemporaryDirectory
import textwrap
from xml.sax.saxutils import escape

parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
parser.add_argument("--report", required=True, type=Path, help="Directory holding session.txt and logo.png")
parser.add_argument("--output", required=True, type=Path, help="PNG file to write; a .svg beside it is not kept")
parser.add_argument("--overwrite", action="store_true", help="Replace the output file if it exists")
args = parser.parse_args()
if not shutil.which("rsvg-convert"):
    sys.exit("error: rsvg-convert is not on PATH")
if args.output.suffix != ".png":
    sys.exit("error: --output must end in .png")
if args.output.exists() and not args.overwrite:
    sys.exit(f"error: {args.output} exists. Pass --overwrite to replace it.")

heading = re.compile(r"^=== (.+) server · command: (\S+) · tool: (.+) ===$")
sections, current = [], None
for line in (args.report / "session.txt").read_text().splitlines():
    match = heading.match(line)
    if match:
        current = {"server": match[1], "command": match[2], "tool": match[3], "lines": []}
        sections.append(current)
    elif current is not None and line.strip():
        current["lines"].append(line.replace("\t", "    "))
if len(sections) != 7:
    sys.exit(f"error: expected 7 server sections in session.txt, found {len(sections)}")
logo = base64.b64encode((args.report / "logo.png").read_bytes()).decode()

CARD, GAP, LINE, TOP = 470, 16, 21, 92
columns = [sections[:4], sections[4:]]
parts, heights = [], []
for index, column in enumerate(columns):
    x, y = 20 + index * (CARD + GAP), TOP
    for section in column:
        body = [chunk for text in section["lines"] for chunk in (textwrap.wrap(text, 50, drop_whitespace=False, replace_whitespace=False) or [text])]
        extra = 96 if section["server"] == "Everything" else 0
        height = 62 + LINE * len(body) + extra
        parts.append(f'<rect x="{x}" y="{y}" width="{CARD}" height="{height}" rx="6" fill="#102b34"/>')
        parts.append(f'<text x="{x + 16}" y="{y + 26}" font-family="sans-serif" font-size="17" font-weight="bold" fill="#ffbd68">{escape(section["server"])} server</text>')
        parts.append(f'<text x="{x + 16}" y="{y + 46}" font-family="monospace" font-size="12" fill="#a9c3c0">command {escape(section["command"])} · tool {escape(section["tool"])}</text>')
        for n, text in enumerate(body):
            parts.append(f'<text x="{x + 16}" y="{y + 72 + n * LINE}" font-family="monospace" font-size="14" fill="#ebefdb" xml:space="preserve">{escape(text)}</text>')
        if extra:
            ty = y + 72 + len(body) * LINE
            parts.append(f'<image x="{x + 16}" y="{ty}" width="80" height="80" image-rendering="optimizeSpeed" xlink:href="data:image/png;base64,{logo}"/>')
            parts.append(f'<text x="{x + 112}" y="{ty + 44}" font-family="sans-serif" font-size="13" fill="#a9c3c0">the saved logo.png, enlarged 4 times</text>')
        y += height + GAP
    heights.append(y)
height = max(heights) + 56
svg = f"""<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="1000" height="{height}" viewBox="0 0 1000 {height}">
<rect width="1000" height="{height}" fill="#163641"/>
<text x="20" y="42" font-family="serif" font-size="30" fill="#fffcea">Seven MCP servers, one Computer session</text>
<text x="20" y="68" font-family="sans-serif" font-size="14" fill="#c3ddc3">Each card is one server's actual output from session.txt, with the text unchanged.</text>
{''.join(parts)}
<text x="20" y="{height - 32}" font-family="sans-serif" font-size="12" fill="#a9c3c0">Drawn by examples/reference-servers/render.py from the session.txt and logo.png that one run saved.</text>
<text x="20" y="{height - 14}" font-family="sans-serif" font-size="12" fill="#a9c3c0">Fetch read the public example.com page; the other data is the synthetic ferry timetable.</text>
</svg>"""
args.output.parent.mkdir(parents=True, exist_ok=True)
with TemporaryDirectory() as temp:
    path = Path(temp) / "card.svg"
    path.write_text(svg, encoding="utf-8")
    subprocess.run(["rsvg-convert", "-w", "1000", str(path), "-o", str(args.output)], check=True)
print(f"Wrote {args.output} from {len(sections)} server sections.")
