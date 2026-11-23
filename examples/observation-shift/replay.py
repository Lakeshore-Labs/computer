"""Rebuild the saved shift inside Computer and verify its exported database independently."""

import argparse
import os
from pathlib import Path
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent


def replay(args):
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    runtime = args.python_dir.resolve()
    source = args.source.resolve()
    with tempfile.TemporaryDirectory() as home:
        subprocess.run(
            [
                str(args.computer.resolve()),
                "-python",
                str(runtime / "python.wasm"),
                "-python-lib",
                str(runtime / "lib"),
                "-mount",
                str(source) + ":/source:ro",
                "-mount",
                str(HERE / "recipe") + ":/recipe:ro",
                "-mount",
                str(output) + ":/out",
                "-workdir",
                "/out",
                "-c",
                "python3 /recipe/publish.py /source /out",
            ],
            check=True,
            env={"HOME": home, "PATH": os.defpath},
        )
    subprocess.run(
        [
            sys.executable,
            "-I",
            str(HERE / "check/audit.py"),
            "--input",
            str(source),
            "--output",
            str(output),
            "--report",
            str(output / "audit.json"),
        ],
        check=True,
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ("computer", "python-dir", "output"):
        parser.add_argument("--" + key, type=Path, required=True)
    parser.add_argument("--source", type=Path, default=HERE / "source")
    replay(parser.parse_args())
