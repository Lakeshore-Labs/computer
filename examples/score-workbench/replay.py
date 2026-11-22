"""Use Computer and the official gopls MCP server to repair a small score package."""

import argparse
import asyncio
import contextlib
import difflib
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

HERE = Path(__file__).resolve().parent
PACKAGE = "example.test/scoredesk/clarinet"
OLD = 'return concert + "2"'
NEW = "return concert + WrittenOffset"


@contextlib.asynccontextmanager
async def client(command, args, env, cwd):
    params = StdioServerParameters(command=command, args=args, env=env, cwd=str(cwd))
    async with stdio_client(params) as (rd, wr), ClientSession(rd, wr) as session:
        await session.initialize()
        yield session


async def ask(session, tool, **arguments):
    result = await session.call_tool(tool, arguments)
    text = "\n".join(item.text for item in result.content if item.type == "text")
    if result.is_error:
        raise ValueError(f"{tool} returned an error: {text}")
    return text


async def go_tool(session, tool, **arguments):
    return await ask(
        session,
        "bash",
        command="gopls " + tool + " --json " + shlex.quote(json.dumps(arguments)),
    )


def hashes(path):
    return {
        p.relative_to(path).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in sorted(path.rglob("*"))
        if p.is_file()
    }


async def run(args, tmp):
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    project = out / "score desk"
    shutil.copytree(HERE / "seed", project)
    source = project / "clarinet/clarinet.go"
    main = project / "cmd/main.go"
    go, gopls = shutil.which("go"), shutil.which("gopls")
    if not go or not gopls:
        raise FileNotFoundError("Install Go and gopls and put both on PATH.")
    env = {
        "HOME": str(tmp),
        "PATH": os.environ["PATH"],
        "GOPROXY": "off",
        "GOSUMDB": "off",
        "GOTELEMETRY": "off",
        "GOENV": "off",
        "GOTOOLCHAIN": "local",
        "GOPATH": str(tmp / "gopath"),
        "GOCACHE": str(tmp / "gocache"),
    }

    def native(*command, directory=project):
        completed = subprocess.run(
            command, cwd=directory, env=env, capture_output=True, text=True, timeout=120
        )
        return {
            "exit": completed.returncode,
            "text": (completed.stdout + completed.stderr)
            .replace(str(out), "<workspace>")
            .replace(str(tmp), "<scratch>"),
        }

    def public(text):
        return text.replace(str(out), "<workspace>").replace(str(tmp), "<scratch>")

    config = tmp / "computer.json"
    # gopls resolves its module from its working directory.
    config.write_text(
        json.dumps(
            {
                "mounts": [str(project)],
                "mcpServers": {
                    "gopls": {
                        "command": sys.executable,
                        "args": [
                            "-I",
                            "-c",
                            "import os,sys; os.chdir(sys.argv[1]); os.execv(sys.argv[2],[sys.argv[2],'mcp'])",
                            str(project),
                            gopls,
                        ],
                        "env": env,
                    }
                },
            }
        )
    )
    before = hashes(project)
    initial = source.read_text()
    bad = native(go, "test", "-count=1", "./...")
    assert bad["exit"] and "mismatched types int and untyped string" in bad["text"]
    computer = str(args.computer.resolve())
    async with client(computer, ["-config", str(config), "mcp"], env, tmp) as session:
        outer = sorted(t.name for t in (await session.list_tools()).tools)
        assert outer == ["bash", "edit", "read", "write"]
        working = await go_tool(session, "go-workspace")
        diagnostics = await go_tool(session, "go-diagnostics", files=[str(source)])
        assert "mismatched types int and untyped string" in diagnostics
        search = await go_tool(session, "go-search", query="Written")
        refs = await go_tool(
            session, "go-symbol-references", file=str(main), symbol="clarinet.Written"
        )
        api = await go_tool(session, "go-package-api", packagePaths=[PACKAGE])
        assert (
            all(s in refs for s in ["main.go", "clarinet_test.go"])
            and "func Written(concert int) int" in api
        )
        await ask(session, "edit", path=str(source), oldText=OLD, newText=NEW)
        same = await go_tool(session, "go-diagnostics", files=[str(source)])
    async with client(computer, ["-config", str(config), "mcp"], env, tmp) as second:
        fresh = await go_tool(second, "go-diagnostics", files=[str(source)])
    assert "No diagnostics." in same and "No diagnostics." in fresh
    after = hashes(project)
    assert (
        sorted(k for k in before if before[k] != after[k]) == ["clarinet/clarinet.go"]
        and before.keys() == after.keys()
    )
    tools = [
        native(go, "build", "./..."),
        native(go, "vet", "./..."),
        native(go, "test", "-count=1", "./..."),
        native(go, "run", "./cmd"),
    ]
    assert all(not x["exit"] for x in tools) and "MIDI 62" in tools[-1]["text"]
    control = tmp / "wrong note"
    shutil.copytree(project, control)
    wrong = control / "clarinet/clarinet.go"
    wrong.write_text(wrong.read_text().replace(NEW, "return concert + 3"))
    wrong_build = native(go, "build", "./...", directory=control)
    wrong_test = native(go, "test", "-count=1", "./...", directory=control)
    assert (
        wrong_build["exit"] == 0
        and wrong_test["exit"] != 0
        and "want 62" in wrong_test["text"]
    )
    patch = "".join(
        difflib.unified_diff(
            initial.splitlines(True),
            source.read_text().splitlines(True),
            fromfile="before/clarinet.go",
            tofile="after/clarinet.go",
            n=0,
        )
    )
    receipt = {
        "outer_tools": outer,
        "go": native(go, "version")["text"].strip(),
        "gopls": native(gopls, "version")["text"].strip(),
        "initial_compiler": bad,
        "diagnostics_before": public(diagnostics),
        "workspace_seen": str(project) in working or "scoredesk" in working,
        "symbol_found": "Written" in search,
        "references": public(refs),
        "package_api": public(api),
        "diff": patch,
        "same_session": public(same),
        "fresh_session": public(fresh),
        "after_checks": tools,
        "unchanged_files": [f for f in before if before[f] == after[f]],
        "wrong_offset_test": wrong_test,
        "passed": True,
    }
    (out / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    (out / "repair.diff").write_text(patch)
    print(tools[-1]["text"].strip())


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    with tempfile.TemporaryDirectory() as scratch:
        asyncio.run(run(parser.parse_args(), Path(scratch)))
