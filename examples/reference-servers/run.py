"""Start the seven MCP reference servers and run a single Computer session."""
import argparse
import json
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--computer", type=Path, required=True)
parser.add_argument("--output", type=Path, required=True)
parser.add_argument("--node-bin", type=Path, help="Directory of installed reference server executables")
parser.add_argument("--python-bin", type=Path, help="Directory of installed Python reference servers")
args = parser.parse_args()
binary = args.computer.resolve()
output = args.output.resolve()
output.mkdir(exist_ok=True, parents=True)
fixture = here.parent / "ferry/input"

def node_server(name, version, *options):
    if args.node_bin:
        return {"command": str((args.node_bin / ("mcp-server-" + name)).resolve()), "args": list(options)}
    return {"command": "npx", "args": ["--yes", f"@modelcontextprotocol/server-{name}@{version}", *options]}

def python_server(name, *options):
    exe = "mcp-server-" + name
    if args.python_bin:
        return {"command": str((args.python_bin / exe).resolve()), "args": list(options)}
    return {"command": "uvx", "args": ["--from", exe + "==2026.11.18", exe, *options]}

with TemporaryDirectory() as temp:
    temp = Path(temp)
    gitdir = temp / "timetable"
    subprocess.run(["git", "init", "-q", str(gitdir)], check=True)
    (gitdir / "timetable.json").write_bytes((fixture / "boardings.json").read_bytes())
    subprocess.run(["git", "add", "--", "timetable.json"], cwd=gitdir, check=True)
    subprocess.run(["git", "-c", "user.name=Harborside", "-c", "user.email=demo@example.org",
                    "-c", "commit.gpgsign=false", "commit", "-qm", "Add the synthetic timetable."],
                   cwd=gitdir, check=True)
    servers = {
        "files": node_server("filesystem", "2.4.0", "--read-only-dir", str(fixture)),
        "everything": node_server("everything", "2.3.1", "stdio"),
        "memory": node_server("memory", "2.2.1"),
        "steps": node_server("sequential-thinking", "2.2.0"),
        "clock": python_server("time", "--local-timezone", "UTC"),
        "historybook": python_server("git", "--repository", str(gitdir)),
        "webpage": python_server("fetch", "--allowed-hosts", "example.com"),
    }
    servers["memory"]["env"] = {"MEMORY_FILE_PATH": str(temp / "memory.jsonl")}
    servers["steps"]["env"] = {"DISABLE_THOUGHT_LOGGING": "true"}
    config = temp / "computer.json"
    config.write_text(json.dumps({"mounts": [f"{output}:/work"], "mcpServers": servers}))
    subprocess.run([str(binary), "-config", str(config), "-c", (here / "session.sh").read_text()],
                   cwd=gitdir, check=True)
