"""Rebuild a six-story printable PDF from the original showcase captures."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from urllib.request import urlopen

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("computer", "python-dir", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument(
        "--wheel", type=Path, help="an existing pinned Segno wheel for offline use"
    )
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    pin = json.loads((HERE.parent / "claim-ticket/source.json").read_text())
    with tempfile.TemporaryDirectory() as temp:
        work = Path(temp).resolve()
        thumbs, vendor = work / "thumbs", work / "vendor"
        vendor.mkdir()
        if args.wheel:
            wheel = args.wheel.read_bytes()
        else:
            with urlopen(pin["url"], timeout=30) as response:
                wheel = response.read()
        if hashlib.sha256(wheel).hexdigest() != pin["sha256"]:
            raise ValueError("Segno checksum differs from the pinned wheel")
        (vendor / pin["name"]).write_bytes(wheel)
        subprocess.run(
            [
                sys.executable,
                "-I",
                str(HERE / "recipe/prepare.py"),
                str(HERE.parent),
                str(thumbs),
            ],
            check=True,
        )
        args.python_dir = args.python_dir.resolve()
        subprocess.run(
            [
                str(args.computer.resolve()),
                "-python",
                str(args.python_dir / "python.wasm"),
                "-python-lib",
                str(args.python_dir / "lib"),
                "-mount",
                str(HERE / "recipe") + ":/recipe:ro",
                "-mount",
                str(thumbs) + ":/thumbs:ro",
                "-mount",
                str(vendor) + ":/vendor:ro",
                "-mount",
                str(output) + ":/out",
                "-workdir",
                "/out",
                "-c",
                "python3 /recipe/print.py",
            ],
            env={"HOME": str(work), "PATH": os.defpath},
            check=True,
        )
        (output / "crop-sources.json").write_bytes(
            (thumbs / "sources.json").read_bytes()
        )


if __name__ == "__main__":
    main()
