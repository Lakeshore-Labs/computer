"""Independently check selected native data formats after a complete workbook run."""

import csv
import json
from pathlib import Path
import sqlite3
import sys
import tarfile
import wave
from xml.etree import ElementTree
from zipfile import ZipFile

workspace = Path(sys.argv[1])
fixtures = Path(__file__).with_name("fixtures")
with (fixtures / "day.csv").open(newline="") as table:
    timetable = list(csv.DictReader(table))

with tarfile.open(workspace / "archive-checksum/route-inputs.tar.gz") as packet:
    for name in ("day.csv", "routes.yaml"):
        assert packet.extractfile(name).read() == (fixtures / name).read_bytes()

with sqlite3.connect(
    (workspace / "million-records/million.db").resolve().as_uri() + "?mode=ro", uri=True
) as db:
    assert db.execute("PRAGMA integrity_check").fetchone() == ("ok",)
    totals = db.execute(
        "SELECT route,count(*),sum(riders) FROM trips GROUP BY route ORDER BY route"
    ).fetchall()
assert sum(n for _, n, _ in totals) == 1_000_000
with (workspace / "million-records/million-summary.csv").open() as table:
    rows = sorted(
        (r["route"], int(r["trips"]), int(r["boardings"]))
        for r in csv.DictReader(table)
    )
assert rows == totals

ns = {"s": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
with ZipFile(workspace / "xlsx-brief/timetable.xlsx") as book:
    sheet = ElementTree.fromstring(book.read("xl/worksheets/sheet1.xml"))
    for index, departure in enumerate(timetable, 2):
        cell = sheet.find(f'.//s:c[@r="E{index}"]', ns)
        assert cell.find("s:f", ns).text == f"SUM(B{index}:D{index})"
        assert int(cell.find("s:v", ns).text) == sum(
            int(v) for k, v in departure.items() if k != "hour"
        )
    assert "xl/charts/chart1.xml" in book.namelist()

with wave.open(str(workspace / "sonify/harbor.wav"), "rb") as track:
    assert (track.getframerate(), track.getnchannels(), track.getsampwidth()) == (
        16_000,
        1,
        2,
    )
    expected_samples = round(16_000 * 0.62) * len(timetable)
    assert track.getnframes() == expected_samples

records = json.loads((workspace / "results.json").read_text())
assert {r["id"] for r in records} == {
    r["id"] for r in json.loads(Path(__file__).with_name("recipes.json").read_text())
}
assert all(r["passed"] for r in records)
print(
    f"Verified {len(records)} recipes, 1,000,000 database rows, {len(timetable)} workbook formulas and {expected_samples:,} audio samples."
)
