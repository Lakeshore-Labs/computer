"""Embed the actual crop bytes with their read offsets for an offline pixel inspector."""

import base64
import json
from pathlib import Path

data = json.loads(Path("windows.json").read_bytes())
for w in data["windows"]:
    w["png"] = base64.b64encode(Path(w["id"] + ".png").read_bytes()).decode()
s = (
    Path("/recipe/reader.html")
    .read_text()
    .replace(
        "{{CAPTURE}}", json.dumps(data, separators=(",", ":")).replace("<", "\\u003c")
    )
)
Path("lagoon.html").write_text(s)
