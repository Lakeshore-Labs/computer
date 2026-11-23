"""Chart a captured inventory or count pages from the official MCP registry."""
import argparse
import csv
import hashlib
import json
import shlex
import shutil
import struct
import subprocess
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlencode

HERE = Path(__file__).resolve().parent
ENDPOINT = "https://registry.modelcontextprotocol.io/v0.1/servers"
CATEGORIES = ("Remote only", "Package only", "Both", "Neither declared")
FIELDS = ("page", "remote_only", "package_only", "both", "neither", "rows", "sha256")


def independent_counts(data):
    counts = dict.fromkeys(FIELDS[1:6], 0)
    for item in data["servers"]:
        server = item["server"]
        category = {
            (True, False): "remote_only", (False, True): "package_only",
            (True, True): "both", (False, False): "neither",
        }[bool(server.get("remotes")), bool(server.get("packages"))]
        counts[category] += 1
        counts["rows"] += 1
    return counts


def fetch_pages(binary, output, limit):
    seen = set()
    cursor = ""
    with tempfile.TemporaryDirectory() as scratch, (output / "page-counts.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, FIELDS, lineterminator="\n")
        writer.writeheader()
        while True:
            query = {"version": "latest", "limit": 100}
            if cursor:
                query["cursor"] = cursor
            url = ENDPOINT + "?" + urlencode(query)
            command = "set -eo pipefail\ncurl -fsS " + shlex.quote(url) + " > page.json\njq -c -f /recipe/count.jq page.json"
            result = subprocess.run(
                [binary, "-mount", scratch + ":/work", "-mount", str(HERE) + ":/recipe:ro",
                 "-allow-url", ENDPOINT, "-timeout", "60s", "-c", command],
                check=True, text=True, capture_output=True, timeout=75,
            )
            raw = (Path(scratch) / "page.json").read_bytes()
            data = json.loads(raw)
            counts = json.loads(result.stdout)
            if counts != independent_counts(data):
                raise ValueError("The guest and independent reader counted the page differently.")
            writer.writerow({"page": len(seen) + 1, **counts, "sha256": hashlib.sha256(raw).hexdigest()})
            cursor = data["metadata"].get("nextCursor")
            if not cursor:
                return True
            if cursor in seen:
                raise ValueError("The registry returned a repeated cursor.")
            seen.add(cursor)
            if limit and len(seen) >= limit:
                return False
            time.sleep(0.65)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--live-pages", type=int, metavar="N", help="count up to N live pages")
    mode.add_argument("--live-all", action="store_true", help="continue through the last registry cursor")
    args = parser.parse_args()
    if args.live_pages is not None and args.live_pages < 1:
        parser.error("--live-pages must be positive")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    binary = str(args.computer.resolve())
    if args.live_pages or args.live_all:
        source = ENDPOINT
        complete = fetch_pages(binary, output, args.live_pages)
        observed = datetime.now(timezone.utc).isoformat()
    else:
        capture = json.loads((HERE / "sample" / "capture.json").read_text())
        source, observed, complete = capture["source"], capture["observed_utc"], capture["complete_pagination"]
        shutil.copyfile(HERE / "sample" / "page-counts.csv", output / "page-counts.csv")
    subprocess.run([binary, "-mount", str(output) + ":/work", "-c", (HERE / "chart.sh").read_text()], check=True)
    with (output / "page-counts.csv").open(newline="") as stream:
        pages = list(csv.DictReader(stream))
    expected = dict.fromkeys(CATEGORIES, 0)
    for number, row in enumerate(pages, 1):
        values = [int(row[key]) for key in FIELDS[1:5]]
        if number != int(row["page"]) or min(values) < 0 or sum(values) != int(row["rows"]):
            raise ValueError(f"Invalid counts for page {number}.")
        for label, count in zip(CATEGORIES, values):
            expected[label] += count
    with (output / "distribution.csv").open(newline="") as stream:
        actual = {row["distribution"]: int(row["registrations"]) for row in csv.DictReader(stream)}
    if actual != expected:
        raise ValueError("The guest chart input differs from the independent totals.")
    png = (output / "distribution.png").read_bytes()
    if png[:8] != b"\x89PNG\r\n\x1a\n" or struct.unpack(">II", png[16:24]) != (800, 500):
        raise ValueError("The expected 800 by 500 chart was not produced.")
    receipt = {"source": source, "observed_utc": observed, "complete_pagination": complete,
               "pages": len(pages), "registrations": sum(actual.values()), "distribution": actual}
    (output / "result.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    main()
