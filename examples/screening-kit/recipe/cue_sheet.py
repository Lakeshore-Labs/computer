"""Read the verified screening archive and write a projectionist's chapter list."""

import csv
import hashlib
import json
from pathlib import Path
from zipfile import ZipFile

root = Path("/out")
kit = root / "computer-screening-kit.zip"
expected = json.loads(Path("/release/receipt.json").read_text())
if hashlib.sha256(kit.read_bytes()).hexdigest() != expected["sha256"]:
    raise SystemExit("The downloaded screening kit differs from the published edition.")

with ZipFile(kit) as archive, (root / "cues.csv").open("w", newline="") as out:
    writer = csv.writer(out, lineterminator="\n")
    writer.writerow(("film", "chapter", "start_seconds", "title"))
    for folder, film in (
        ("field-guide", "Inside Computer"),
        ("working-files", "Where the files go"),
    ):
        chapters = json.loads(archive.read(f"{folder}/chapters.json"))
        for number, section in enumerate(chapters, 1):
            writer.writerow((film, number, f"{section['start']:.3f}", section["title"]))
print((root / "cues.csv").read_text(), end="")
