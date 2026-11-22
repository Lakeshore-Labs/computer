"""Build a portable Oslo bike station map and optionally find ten bikes in a native GeoPackage."""

import argparse
import hashlib
import json
import shlex
import subprocess
from datetime import UTC
from pathlib import Path
from zipfile import ZipFile

HERE = Path(__file__).resolve().parent
BASE = "https://gbfs.urbansharing.com/oslobysykkel.no/"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for flag in ("computer", "python-dir", "output"):
        parser.add_argument("--" + flag, type=Path, required=True)
    parser.add_argument(
        "--gis",
        action="store_true",
        help="read with Fiona/GDAL and publish a group meeting report",
    )
    parser.add_argument(
        "--live",
        action="store_true",
        help="append a fresh Oslo City Bike feed snapshot",
    )
    args = parser.parse_args()
    computer = args.computer.resolve()
    runtime = args.python_dir.resolve()
    out = args.output.resolve()
    capture = HERE / "source/capture.zip"
    manifest = json.loads((HERE / "source/manifest.json").read_text())
    if hashlib.sha256(capture.read_bytes()).hexdigest() != manifest["archive_sha256"]:
        raise ValueError("capture checksum differs")
    out.mkdir(parents=True, exist_ok=False)
    inputs = out / "inputs"
    inputs.mkdir()
    with ZipFile(capture) as z:
        if sorted(z.namelist()) != sorted(manifest["members"]):
            raise ValueError("capture members differ from the manifest")
        for name, checksum in manifest["members"].items():
            data = z.read(name)
            if hashlib.sha256(data).hexdigest() != checksum:
                raise ValueError("capture member differs: " + name)
            dest = inputs / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data)
    source = inputs / "observations"
    if args.live:
        from datetime import datetime

        key = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
        result = source / key
        result.mkdir()
        # Fetch both inputs from the same provider. Newly added stations should be visible.
        for fn, dest in [
            ("station_information.json", source),
            ("station_status.json", result),
        ]:
            url = BASE + fn
            cmd = (
                "curl -fsS -H 'Client-Identifier: lakeshorelabs-computerfieldguide' "
                + shlex.quote(url)
                + " > "
                + fn
            )
            subprocess.run(
                [
                    computer,
                    "-mount",
                    str(dest) + ":/work",
                    "-allow-url",
                    url,
                    "-timeout",
                    "60s",
                    "-c",
                    cmd,
                ],
                check=True,
            )
        (result / "receipt.json").write_text(
            json.dumps({"guest_exit": 0, "requested_utc": key}) + "\n"
        )
    host = [
        computer,
        "-mount",
        str(out) + ":/work",
        "-mount",
        str(HERE / "recipe") + ":/recipe:ro",
        "-mount",
        str(source) + ":/source:ro",
    ]
    for name in ["vendor", "static", "licenses"]:
        host.extend(["-mount", str(inputs / name) + ":/" + name + ":ro"])
    host.extend(
        [
            "-python",
            runtime / "python.wasm",
            "-python-lib",
            runtime / "lib",
            "-timeout",
            "120s",
        ]
    )
    subprocess.run([*host, "-c", "python3 /recipe/publish.py"], check=True)
    if args.gis:
        from project import project

        native = project(out, source)
        subprocess.run([*host, "-c", "python3 /recipe/meet.py"], check=True)
        (out / "native.json").write_text(json.dumps(native, indent=2) + "\n")


if __name__ == "__main__":
    main()
