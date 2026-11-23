"""Run the screening kit packager in a stock Computer with a read-only examples mount."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
from zipfile import ZipFile
import hashlib

HERE = Path(__file__).resolve().parent
p = argparse.ArgumentParser(description=__doc__)
for k in ("computer", "python-dir", "output"):
    p.add_argument("--" + k, type=Path, required=True)
a = p.parse_args()
out = a.output.resolve()
out.mkdir(parents=True, exist_ok=False)
runtime = a.python_dir.resolve()
with tempfile.TemporaryDirectory() as h:
    raw = subprocess.check_output(
        [
            str(a.computer.resolve()),
            "-python",
            str(runtime / "python.wasm"),
            "-python-lib",
            str(runtime / "lib"),
            "-mount",
            str(HERE.parent) + ":/examples:ro",
            "-mount",
            str(out) + ":/out",
            "-workdir",
            "/out",
            "-c",
            "python3 /examples/screening-kit/recipe/package.py",
        ],
        env={"HOME": h, "PATH": os.defpath},
    )
info = json.loads(raw)
bundle = out / "computer-screening-kit.zip"
with ZipFile(bundle) as z:
    assert (
        z.testzip() is None
        and len(z.namelist()) == len(set(z.namelist())) == info["files"]
    )
    checks = json.loads(z.read("checksums.json"))
    originals = {
        "index.html": HERE / "recipe/start.html",
        "field-guide.pdf": HERE.parent / "pocket-guide/sample/pocket-guide.pdf",
        "integration-map.html": HERE.parent / "integration-map/sample/map.html",
    }
    for folder, source in [
        ("field-guide", "field-film/sample"),
        ("working-files", "field-film/working-files/sample"),
    ]:
        for name in (
            "computer.mp4",
            "poster.jpg",
            "index.html",
            "chapters.json",
            "captions.vtt",
            "transcript.md",
        ):
            originals[folder + "/" + name] = HERE.parent / source / name
    assert checks.keys() == originals.keys()
    for member, path in originals.items():
        data = z.read(member)
        assert data == path.read_bytes() and checks[member] == {
            "bytes": len(data),
            "sha256": hashlib.sha256(data).hexdigest(),
        }
assert hashlib.sha256(bundle.read_bytes()).hexdigest() == info["sha256"]
(out / "receipt.json").write_text(
    json.dumps({**info, "originals_compared": len(originals)}, indent=2) + "\n"
)
print(info)
