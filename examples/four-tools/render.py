"""Render a visual walkthrough from a saved trace and the chart its client received."""
import argparse
import base64
import hashlib
from html import escape
import json
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("report", type=Path, help="Directory written by replay.py")
parser.add_argument("output", type=Path, help="Destination PNG")
args = parser.parse_args()
trace = json.loads((args.report / "trace.json").read_text())
control, attached, shell, image = trace["events"]
picture = (args.report / "report.png").read_bytes()
if hashlib.sha256(picture).hexdigest() != image["sha256"] or control["tools"] != attached["tools"]:
    raise ValueError("The picture or tool lists differ from the saved trace.")
encoded = base64.b64encode(picture).decode("ascii")
labels = [
    ("Client tool list", "  ·  ".join(control["tools"]), "Same names with zero or two upstream servers."),
    ("In the guest", f'{trace["input_rows"]:,} input rows', f'{trace["input_bytes"]:,} CSV bytes fetched by WASI Python.'),
    ("Back to the client", f'{shell["result_text_bytes"]} text bytes', "The client retrieved the chart in a separate read."),
]
blocks = []
for i, (heading, value, detail) in enumerate(labels):
    y = 165 + 126 * i
    blocks.append(f'<line x1="28" x2="502" y1="{y-14}" y2="{y-14}" stroke="#b8c7bb"/>'
                  f'<text x="28" y="{y+10}" font-size="16" fill="#387363">{escape(heading)}</text>'
                  f'<text x="28" y="{y+48}" font-size="29">{escape(value)}</text>'
                  f'<text x="28" y="{y+77}" font-size="17">{escape(detail)}</text>')
svg = f"""<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="1100" height="570">
<rect width="1100" height="570" fill="#f7f4e9"/><rect width="1100" height="112" fill="#153e38"/>
<text x="28" y="33" font-family="sans-serif" font-size="16" fill="#b7d5bf">COMPUTER / MEASURED MCP EXECUTION</text>
<text x="28" y="82" font-family="serif" font-size="40" fill="#fff9e8">Where the data goes</text>
<g font-family="sans-serif" fill="#193f38">{''.join(blocks)}
<text x="532" y="170" font-size="18">What the client received</text>
<image x="532" y="195" width="542" height="320" xlink:href="data:image/png;base64,{encoded}"/>
<text x="532" y="544" font-size="16">{image['dimensions'][0]} × {image['dimensions'][1]} pixels · {image['bytes']:,} PNG bytes</text>
</g></svg>"""
with TemporaryDirectory() as temp:
    drawing = Path(temp) / "trace.svg"
    drawing.write_text(svg)
    subprocess.run(["rsvg-convert", str(drawing), "-o", str(args.output)], check=True)
