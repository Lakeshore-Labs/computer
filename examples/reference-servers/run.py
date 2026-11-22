"""Start the seven MCP reference servers and run a single Computer session.

Who runs what: you start this script on the host. It starts the servers on the host (npx and uvx, or
the installed executables you name), starts Computer, and Computer runs session.sh. Afterwards this
script compares the transcript with media/session.txt.
"""
import argparse
import difflib
import json
import os
import shlex
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--computer", type=Path, required=True)
parser.add_argument("--output", type=Path, required=True, help="Directory for session.txt and logo.png")
parser.add_argument("--node-bin", type=Path, help="Directory of installed reference server executables")
parser.add_argument("--python-bin", type=Path, help="Directory of installed Python reference servers")
args = parser.parse_args()
binary = args.computer.resolve()
output = args.output.resolve()
if not binary.is_file():
    sys.exit(f"error: {args.computer} is not a file. Build it first: CGO_ENABLED=0 go build -o ./bin/computer ./cmd/computer")
if ":" in str(output):
    sys.exit(f"error: the output path may not contain ':' because Computer reads it as host:guest: {output}")
if output.exists() and not output.is_dir():
    sys.exit(f"error: {output} exists and is not a directory")
clash = [name for name in ("session.txt", "logo.png") if (output / name).exists()]
if clash:
    parser.error("Choose a fresh output directory for session.txt and logo.png.")
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
    # Keep the host's global Git configuration and hooks out of the fixture repository.
    git_env = {**os.environ, "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}
    subprocess.run(["git", "-c", "core.hooksPath=" + os.devnull, "init", "-q", str(gitdir)], check=True, env=git_env)
    (gitdir / "timetable.json").write_bytes((fixture / "boardings.json").read_bytes())
    subprocess.run(["git", "add", "--", "timetable.json"], cwd=gitdir, check=True, env=git_env)
    subprocess.run(["git", "-c", "core.hooksPath=" + os.devnull, "-c", "user.name=Harborside", "-c", "user.email=demo@example.org",
                    "-c", "commit.gpgsign=false", "commit", "-qm", "Add the synthetic timetable."],
                   cwd=gitdir, check=True, env=git_env)
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
    script = (here / "session.sh").read_text().replace("@REPO@", shlex.quote(str(gitdir)))
    run = subprocess.run([str(binary), "-config", str(config), "-c", script],
                         stdout=subprocess.PIPE, text=True, cwd=gitdir)
sys.stdout.write(run.stdout)
if run.returncode:
    sys.exit(run.returncode)
(output / "session.txt").write_text(run.stdout)
expected = (here / "media/session.txt").read_text()
if run.stdout != expected:
    sys.stderr.write("".join(difflib.unified_diff(expected.splitlines(True), run.stdout.splitlines(True),
                                                   "media/session.txt", "this run")))
    sys.exit("error: this run's transcript differs from media/session.txt")
print("Transcript matches media/session.txt.")
