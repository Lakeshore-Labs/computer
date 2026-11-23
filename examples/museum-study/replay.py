"""Build the Saint-Rémy collection study through a native rclone VFS mount."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

from verify import check

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--live-computer", type=Path, help="fetch a fresh copy of the two works using this CLI")
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    source = HERE / "sources"
    if args.live_computer:
        source = out / "sources"
        for museum in ("cleveland", "getty"):
            (source / museum).mkdir(parents=True)
        recipe = (HERE / "recipe/fetch.sh").read_text()
        # The guest's fetch recipe declares each literal source URL.
        urls = [line.split("'")[1] for line in recipe.splitlines() if line.startswith("curl -fsS '")]
        cmd = [str(args.live_computer.resolve()), "-mount", str(source) + ":/work"]
        for url in urls:
            cmd.extend(["-allow-url", url])
        subprocess.run([*cmd, "-c", recipe], check=True)
    with tempfile.TemporaryDirectory() as temp:
        binary = Path(temp) / "museum-host"
        subprocess.run(["go", "build", "-mod=readonly", "-o", str(binary), "."], cwd=HERE / "host",
                       env={**os.environ, "CGO_ENABLED": "0"}, check=True)
        with (out / "guest.log").open("w") as log:
            subprocess.run([str(binary), str(source), str(HERE / "recipe"), str(out)], stdout=log, check=True)
    receipt = check(source, out)
    (out / "verification.json").write_text(json.dumps(receipt, indent=2, ensure_ascii=False) + "\n")
    print(json.dumps(receipt, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
