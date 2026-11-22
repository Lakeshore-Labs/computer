"""Issue a synthetic ticket in Computer and independently read its QR code three ways."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import urllib.request

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--python-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--wheel", type=Path, help="use an existing pinned Segno wheel")
    parser.add_argument("--degrees", type=int, default=90)
    args = parser.parse_args()
    computer, runtime, out = (
        p.resolve() for p in (args.computer, args.python_dir, args.output)
    )
    if any(":" in str(p) for p in (out, HERE)):
        parser.error("Computer mount paths cannot contain a colon")
    if (
        not computer.is_file()
        or not (runtime / "python.wasm").is_file()
        or not (runtime / "lib").is_dir()
    ):
        parser.error("provide a Computer binary and its WASI Python release directory")
    if not shutil.which("pdftoppm"):
        parser.error("install Poppler for the independent PDF scan")
    out.mkdir(parents=True, exist_ok=False)
    expected = json.loads((HERE / "recipe/lost-property.json").read_text())["payload"]
    pin = json.loads((HERE / "source.json").read_text())
    with tempfile.TemporaryDirectory() as tmp:
        vendor = Path(tmp).resolve()
        if ":" in str(vendor):
            parser.error("the temporary path cannot contain a colon")
        if args.wheel:
            wheel = args.wheel.read_bytes()
        else:
            with urllib.request.urlopen(pin["url"], timeout=30) as response:
                wheel = response.read()
        if hashlib.sha256(wheel).hexdigest() != pin["sha256"]:
            raise ValueError("Segno wheel checksum differs from the pinned release")
        (vendor / pin["name"]).write_bytes(wheel)
        manifest = HERE / "client/Cargo.toml"
        build = [
            "cargo",
            "build",
            "--locked",
            "--manifest-path",
            str(manifest),
            "--message-format=json",
        ]
        env = dict(os.environ)
        env.setdefault("CARGO_TARGET_DIR", str(vendor / "target"))
        build_output = subprocess.check_output(build, cwd=HERE, env=env, text=True)
        binaries = [
            row["executable"]
            for line in build_output.splitlines()
            if (row := json.loads(line)).get("executable")
            and row["target"]["name"] == "claim-reader"
        ]
        if len(binaries) != 1:
            raise ValueError("Cargo did not return one claim-reader executable")
        client = binaries[0]
        result = subprocess.check_output(
            [
                client,
                computer,
                out,
                HERE / "recipe",
                vendor,
                runtime,
                str(args.degrees),
            ],
            text=True,
        )
        scans = json.loads(result)
        subprocess.run(
            [
                "pdftoppm",
                "-r",
                "144",
                "-png",
                "-singlefile",
                out / "claim.pdf",
                out / "paper",
            ],
            check=True,
        )
        paper = subprocess.check_output(
            [client, "scan", out / "paper.png"], text=True
        ).strip()
        if paper != expected or any(
            scan["decoded"] != expected for scan in scans["scans"]
        ):
            raise ValueError("one of the scans returned a different claim")
        scans["rendered_pdf"] = {"dpi": 144, "decoded": paper}
        scans["sha256"] = {
            name: hashlib.sha256((out / name).read_bytes()).hexdigest()
            for name in ("claim.pdf", "claim.png", "rotated.png", "paper.png")
        }
        (out / "checks.json").write_text(json.dumps(scans, indent=2) + "\n")
        print(f"Issued {expected} and verified all three scans.")


if __name__ == "__main__":
    main()
