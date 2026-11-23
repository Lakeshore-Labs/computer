"""Validate a model's label and produce a local ZPL print job from matching fields."""

import hashlib
import json
from pathlib import Path
import re
import sys

source = Path("/orders/today.txt")
desk = Path("/desk")
fields = dict(
    line.split(": ", 1) for line in source.read_text().splitlines() if ": " in line
)
shelf, ribbon = fields["Shelf"], fields["Pickup ribbon"]
if not re.fullmatch(r"[A-Z]+-[0-9]{2}", shelf) or not re.fullmatch(r"[A-Z]+", ribbon):
    raise ValueError("The source fields do not match the studio label format.")
wanted = shelf + " " + ribbon
draft = (
    (desk / "label.txt").read_text().strip() if (desk / "label.txt").exists() else ""
)
mode = sys.argv[1]
if mode not in ("inspect", "correct"):
    raise ValueError("Choose inspect or correct.")
target = desk / "ticket.zpl"
if target.exists():
    raise FileExistsError("Choose a new queue file.")
if draft == wanted or mode == "correct":
    zpl = f"^XA\n^PW600\n^LL220\n^FO30,30^A0N,30,28^FDWARDROBE PICKUP^FS\n^FO30,100^A0N,65,50^FD{wanted}^FS\n^XZ\n"
    target.write_text(zpl)
    decision = "accepted" if draft == wanted else "corrected from source"
else:
    decision = "held"
result = {
    "mode": mode,
    "decision": decision,
    "source_label": wanted,
    "draft": draft,
    "queue_file_exists": target.exists(),
    "zpl_sha256": hashlib.sha256(target.read_bytes()).hexdigest()
    if target.exists()
    else None,
}
(desk / (mode + ".json")).write_text(json.dumps(result, indent=2) + "\n")
print(decision)
