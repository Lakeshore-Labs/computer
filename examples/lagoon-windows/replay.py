"""Fetch only the header and three compressed image blocks needed for the lagoon reader."""

import argparse
import hashlib
import json
import shlex
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("computer", "python-dir", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    args = parser.parse_args()
    computer, runtime, out = (
        p.resolve() for p in (args.computer, args.python_dir, args.output)
    )
    source = HERE / "source"
    pin = json.loads((source / "probe.json").read_text())
    item = json.loads((source / "item.json").read_text())
    url = item["assets"]["visual"]["href"]
    if url != pin["url"]:
        raise ValueError("STAC object URL differs from the range receipt")
    out.mkdir(parents=True, exist_ok=False)
    guest = [
        computer,
        "-mount",
        str(out) + ":/work",
        "-mount",
        str(source) + ":/source:ro",
        "-mount",
        str(HERE / "recipe") + ":/recipe:ro",
        "-python",
        runtime / "python.wasm",
        "-python-lib",
        runtime / "lib",
        "-timeout",
        "180s",
    ]
    fetch = (
        "curl -fsS -H " + shlex.quote("If-Match: " + pin["etag"]) + " -w '%{http_code}'"
    )
    script = (
        "set -eo pipefail\n"
        + fetch
        + " -H 'Range: bytes=0-16383' -o header.bin "
        + shlex.quote(url)
        + " > header.http\n"
        + 'test "$(cat header.http)" = 206\n'
        + "python3 /recipe/plan.py\n"
        + "while read -r id start end; do\n"
        + "  "
        + fetch
        + ' -H "Range: bytes=$start-$end" -o "block-$id.deflate" '
        + shlex.quote(url)
        + ' > "block-$id.http"\n'
        + '  test "$(cat "block-$id.http")" = 206\n'
        + "done < ranges.tsv\n"
    )
    subprocess.run([*guest, "-allow-url", url, "-c", script], check=True)
    subprocess.run(
        [*guest, "-c", "set -e\npython3 /recipe/decode.py\npython3 /recipe/publish.py"],
        check=True,
    )
    expected = json.loads((HERE / "sample/windows.json").read_text())
    received = json.loads((out / "windows.json").read_text())
    if expected != received:
        raise ValueError("range hashes, crop hashes or source dimensions differ")
    measured = sum(
        (out / f).stat().st_size
        for f in ["header.bin", *(f"block-{i}.deflate" for i in received["tiles"])]
    )
    assert measured == received["image_range_bytes"]
    for w in received["windows"]:
        assert (
            hashlib.sha256((out / (w["id"] + ".png")).read_bytes()).hexdigest()
            == w["png_sha256"]
        )
    print(f"Retrieved {measured:,} GeoTIFF bytes in four HTTP 206 responses.")


if __name__ == "__main__":
    main()
