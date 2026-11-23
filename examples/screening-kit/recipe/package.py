"""Pack the two films and their offline readers inside Computer's WASI Python."""

import hashlib
import json
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZIP_STORED, ZipFile, ZipInfo

examples = Path("/examples")
out = Path("/out")
files = {
    "index.html": (examples / "screening-kit/recipe/start.html").read_bytes(),
    "field-guide.pdf": (examples / "pocket-guide/sample/pocket-guide.pdf").read_bytes(),
    "integration-map.html": (examples / "integration-map/sample/map.html").read_bytes(),
}
for name, folder in [
    ("field-guide", "field-film/sample"),
    ("working-files", "field-film/working-files/sample"),
]:
    for asset in (
        "index.html",
        "poster.jpg",
        "computer.mp4",
        "captions.vtt",
        "transcript.md",
        "chapters.json",
    ):
        files[name + "/" + asset] = (examples / folder / asset).read_bytes()
checks = {
    name: {"bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}
    for name, data in sorted(files.items())
}
files["checksums.json"] = (json.dumps(checks, indent=2) + "\n").encode()
with ZipFile(out / "computer-screening-kit.zip", "w") as archive:
    for name, data in sorted(files.items()):
        entry = ZipInfo(name, date_time=(2026, 11, 22, 0, 0, 0))
        entry.compress_type = (
            ZIP_STORED if name.endswith((".jpg", ".pdf", ".mp4")) else ZIP_DEFLATED
        )
        entry.external_attr = 0o100644 << 16
        archive.writestr(entry, data)
print(
    json.dumps(
        {
            "files": len(files),
            "source_bytes": sum(map(len, files.values())),
            "zip_bytes": (out / "computer-screening-kit.zip").stat().st_size,
            "sha256": hashlib.sha256(
                (out / "computer-screening-kit.zip").read_bytes()
            ).hexdigest(),
        }
    )
)
