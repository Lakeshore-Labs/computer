"""Fetch the published kit through Computer and compare its cue sheet with MP4 metadata."""

import argparse
import csv
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path
from zipfile import ZipFile

HERE = Path(__file__).resolve().parent
URL = (
    "https://raw.githubusercontent.com/Lakeshore-Labs/computer/"
    "70a93921b2b2f46e14e4604ae262684de11e373b/examples/screening-kit/sample/computer-screening-kit.zip"
)


def receive(computer, runtime, output):
    output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory() as temp:
        release = Path(temp) / "release"
        release.mkdir()
        shutil.copyfile(HERE / "sample/receipt.json", release / "receipt.json")
        subprocess.run(
            [
                str(computer),
                "-python",
                str(runtime / "python.wasm"),
                "-python-lib",
                str(runtime / "lib"),
                "-allow-url",
                URL,
                "-mount",
                f"{HERE / 'recipe'}:/recipe:ro",
                "-mount",
                f"{release}:/release:ro",
                "-mount",
                f"{output}:/out",
                "-workdir",
                "/out",
                "-timeout",
                "60s",
                "-c",
                f"set -eo pipefail\ncurl -fsS '{URL}' > computer-screening-kit.zip\npython3 /recipe/cue_sheet.py",
            ],
            env={"HOME": temp, "PATH": os.defpath},
            check=True,
        )

    kit = output / "computer-screening-kit.zip"
    sheet = output / "cues.csv"
    with sheet.open(newline="") as file:
        rows = list(csv.DictReader(file))
    expected = []
    with ZipFile(kit) as archive, tempfile.TemporaryDirectory() as temp:
        for folder, title in (
            ("field-guide", "Inside Computer"),
            ("working-files", "Where the files go"),
        ):
            film = Path(temp) / "film.mp4"
            film.write_bytes(archive.read(f"{folder}/computer.mp4"))
            raw = subprocess.check_output(
                ["ffprobe", "-v", "error", "-show_chapters", "-of", "json", film]
            )
            for number, chapter in enumerate(json.loads(raw)["chapters"], 1):
                expected.append(
                    (
                        title,
                        number,
                        float(chapter["start_time"]),
                        chapter["tags"]["title"],
                    )
                )
    actual = [
        (r["film"], int(r["chapter"]), float(r["start_seconds"]), r["title"])
        for r in rows
    ]
    assert len(expected) == len(actual) == 12
    for (film, number, seconds, title), (
        want_film,
        want_number,
        want_seconds,
        want_title,
    ) in zip(actual, expected):
        assert (film, number, title) == (want_film, want_number, want_title)
        assert abs(seconds - want_seconds) <= 0.001
    receipt = {
        "source": URL,
        "zip_bytes": kit.stat().st_size,
        "zip_sha256": hashlib.sha256(kit.read_bytes()).hexdigest(),
        "cue_sheet_bytes": sheet.stat().st_size,
        "cue_sheet_sha256": hashlib.sha256(sheet.read_bytes()).hexdigest(),
        "checked_chapters": len(actual),
        "largest_difference_seconds": round(
            max(abs(a[2] - b[2]) for a, b in zip(actual, expected)), 6
        ),
    }
    (output / "receiving.json").write_text(json.dumps(receipt, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ("computer", "python-dir", "output"):
        parser.add_argument("--" + key, type=Path, required=True)
    args = parser.parse_args()
    receive(args.computer.resolve(), args.python_dir.resolve(), args.output.resolve())
