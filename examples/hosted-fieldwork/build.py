"""Build the offline browser from the recorded survey."""
import json
from pathlib import Path

root = Path(__file__).resolve().parent
rows = json.loads((root / "sample/survey.json").read_text())
template = (root / "fieldwork.html").read_text()
payload = json.dumps(rows, ensure_ascii=False, separators=(",", ":")).replace("<", "\\u003c")
(root / "sample/fieldwork.html").write_text(template.replace("{{SURVEY}}", payload))
