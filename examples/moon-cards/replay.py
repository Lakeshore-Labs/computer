"""Build two Anki packages in Computer from the original four-moons EPUB."""

import argparse
import os
from pathlib import Path
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("computer", "python-dir", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument(
        "--cache", type=Path, help="local cache for verified Python packages"
    )
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    python = args.python_dir.resolve()
    with tempfile.TemporaryDirectory() as name:
        temp = Path(name).resolve()
        vendor = temp / "vendor"
        subprocess.run(
            [
                sys.executable,
                "-I",
                str(HERE / "recipe/install.py"),
                str(args.cache or temp / "downloads"),
                str(vendor),
            ],
            check=True,
        )
        subprocess.run(
            [
                str(args.computer.resolve()),
                "-python",
                str(python / "python.wasm"),
                "-python-lib",
                str(python / "lib"),
                "-mount",
                str(HERE / "recipe") + ":/recipe:ro",
                "-mount",
                str(vendor) + ":/vendor:ro",
                "-mount",
                str(HERE.parent / "moon-almanac/sample") + ":/book:ro",
                "-mount",
                str(output) + ":/out",
                "-workdir",
                "/out",
                "-c",
                "python3 /recipe/issue.py",
            ],
            check=True,
            env={"HOME": name, "PATH": os.defpath},
        )


if __name__ == "__main__":
    main()
