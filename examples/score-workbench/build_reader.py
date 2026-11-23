"""Build the offline diagnostic comparison from a completed replay."""
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
trace = json.loads((out / "receipt.json").read_text())
template = Path(__file__).with_name("reader.html").read_text()
(out / "workbench.html").write_text(template.replace("__RECEIPT__", json.dumps(trace).replace("<", "\\u003c")))
