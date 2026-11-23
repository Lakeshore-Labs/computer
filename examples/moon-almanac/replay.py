"""Publish and read the pocket astronomy book from a capture or its live sources."""

import argparse
import hashlib
import json
from pathlib import Path
import shlex
import subprocess
from zipfile import ZipFile, ZIP_STORED
from xml.etree import ElementTree as ET

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--python-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--live",
        action="store_true",
        help="fetch Wikimedia MCP and NASA/JPL source data",
    )
    args = parser.parse_args()
    computer, runtime, out = [
        p.resolve() for p in (args.computer, args.python_dir, args.output)
    ]
    if any(":" in str(p) for p in (out, HERE)):
        parser.error("Computer mount paths cannot contain a colon")
    out.mkdir(parents=True, exist_ok=False)
    pin = json.loads((HERE / "sources/receipt.json").read_text())
    source = HERE / "sources"
    if args.live:
        source = out / "sources"
        source.mkdir()
        query = (
            'wd execute-sparql --sparql "$(cat /recipe/moons.rq)" --k 8 > wikidata.csv'
        )
        urls = [
            (pin["jpl_measurements"], "jpl.html"),
            (pin["image_url"], "galilean.jpg"),
        ]
        script = (
            "set -eo pipefail\n"
            + query
            + "\n"
            + "\n".join(
                "curl -fsS " + shlex.quote(url) + " > " + filename
                for url, filename in urls
            )
        )
        host = [
            str(computer),
            "-mount",
            str(source) + ":/work",
            "-mount",
            str(HERE / "recipe") + ":/recipe:ro",
            "-mcp",
            "wd=" + pin["wikidata_mcp"],
            "-timeout",
            "100s",
        ]
        for url, _ in urls:
            host.extend(["-allow-url", url])
        subprocess.run([*host, "-c", script], check=True)
    produced = subprocess.check_output(
        [
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
            "-c",
            "python3 /recipe/publish.py",
        ],
        text=True,
    )
    publication = out / "four-moons.epub"
    with ZipFile(publication) as book:
        first = book.infolist()[0]
        assert (
            first.filename == "mimetype"
            and first.compress_type == ZIP_STORED
            and book.read(first) == b"application/epub+zip"
        )
        assert book.read("EPUB/galilean.jpg") == (source / "galilean.jpg").read_bytes()
        package = ET.fromstring(book.read("EPUB/package.opf"))
        ns = {"o": "http://www.idpf.org/2007/opf", "h": "http://www.w3.org/1999/xhtml"}
        assert [
            c.attrib["idref"] for c in package.findall("o:spine/o:itemref", ns)
        ] == ["cover", "measure", "credits"]
        table = ET.fromstring(book.read("EPUB/measure.xhtml"))
        cells = [[c.text for c in tr] for tr in table.findall(".//h:tbody/h:tr", ns)]
    moons = json.loads((out / "moons.json").read_text())
    assert cells == [
        [r["name"], f"{float(r['radius_km']):,.2f}", "±" + r["sigma_km"]] for r in moons
    ]
    # Pandoc is a separate EPUB reader. Check the decoded page text for each source row.
    text = subprocess.check_output(
        ["pandoc", "-f", "epub", "-t", "plain", publication], text=True
    )
    for row in cells:
        assert all(value in text for value in row)
    expected = (HERE / "sample/four-moons.epub").read_bytes()
    actual = publication.read_bytes()
    if not args.live and actual != expected:
        raise ValueError("captured replay differs from the published EPUB")
    check = {
        "live": args.live,
        "book_bytes": len(actual),
        "book_sha256": hashlib.sha256(actual).hexdigest(),
        "byte_identical_to_sample": actual == expected,
        "spine_chapters": 3,
        "rows": cells,
        "pandoc_read_all_rows": True,
        "guest_output": produced.strip(),
    }
    (out / "checks.json").write_text(json.dumps(check, indent=2) + "\n")
    print(json.dumps(check, indent=2))


if __name__ == "__main__":
    main()
