"""Build the searchable atlas and validate every link between observations and declarations."""
import json
from pathlib import Path

here = Path(__file__).resolve().parent
sample = here / "sample"
atlas = json.loads((sample / "atlas.json").read_text())
rows = atlas["rows"]
declared = {entry["name"] for entry in json.loads((sample / "declarations.json").read_text())}
if declared != {row["name"] for row in rows if row["category"] in ("Go function", "Go method")}:
    raise ValueError("The Go entries differ from the declared functions and methods.")
if len({row["key"] for row in rows}) != len(rows):
    raise ValueError("The atlas contains duplicate entry keys.")
for row in rows:
    for reference in [row["evidence_source"], *row.get("also", [])]:
        if reference.startswith("receipt:") and reference[8:] not in atlas["receipts"]:
            raise ValueError("Missing receipt " + reference)
    row.setdefault("also", [])
content = json.dumps(atlas, ensure_ascii=False).replace("<", "\\u003c")
(sample / "atlas.html").write_text((here / "reader.html").read_text().replace("__ATLAS__", content))
