"""Replay the ferry report in a new or existing output directory."""
import argparse
import json
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--computer", required=True, type=Path, help="Path to the Computer executable")
parser.add_argument("--output", required=True, type=Path, help="Directory for totals.csv and ferry.png")
parser.add_argument("--server", type=Path, help="Use an installed mcp-server-filesystem executable")
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)

server = {"command": "npx", "args": ["--yes", "@modelcontextprotocol/server-filesystem@2.4.0"]}
if args.server:
    server = {"command": str(args.server.resolve()), "args": []}
server["args"] += ["--read-only-dir", str(here / "input")]
config = {
    "mounts": [f"{args.output.resolve()}:/work"],
    "mcpServers": {"files": server},
}
with TemporaryDirectory() as temp:
    path = Path(temp) / "config.json"
    path.write_text(json.dumps(config), encoding="utf-8")
    subprocess.run([str(args.computer.resolve()), "-config", str(path), "-c",
                    (here / "report.sh").read_text(encoding="utf-8")], check=True)
