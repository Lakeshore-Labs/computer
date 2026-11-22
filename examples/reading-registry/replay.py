"""Issue two editions through Computer MCP and retrieve both from a local OCI registry."""

import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import tempfile
import urllib.request
import zipfile
from mcp import ClientSession, StdioServerParameters, stdio_client

HERE = Path(__file__).resolve().parent


def run(*args):
    return subprocess.check_output(
        [str(x) for x in args], text=True, stderr=subprocess.STDOUT, timeout=45
    )


def sha(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


async def build(args, out, work):
    params = StdioServerParameters(
        command=str(args.computer.resolve()),
        args=[
            "-mount",
            str(work) + ":/work",
            "-mount",
            str(args.book_dir.resolve()) + ":/book:ro",
            "-mount",
            str(HERE / "recipe") + ":/recipe:ro",
            "-python",
            str(args.python_dir.resolve() / "python.wasm"),
            "-python-lib",
            str(args.python_dir.resolve() / "lib"),
            "mcp",
        ],
    )
    editions = []
    async with stdio_client(params) as (rd, wr), ClientSession(rd, wr) as client:
        await client.initialize()
        listed = await client.list_tools()
        names = sorted(x.name for x in listed.tools)
        initial = (
            json.dumps(
                {
                    "edition": "original",
                    "sequence": 0,
                    "start": "2026-11-24T17:30:00+00:00",
                },
                indent=2,
            )
            + "\n"
        )
        updated = (
            json.dumps(
                {
                    "edition": "amended",
                    "sequence": 1,
                    "start": "2026-11-24T18:00:00+00:00",
                },
                indent=2,
            )
            + "\n"
        )
        for n, content in enumerate((initial, updated)):
            if n == 0:
                operation = await client.call_tool(
                    "write", {"path": "session.json", "content": content}
                )
            else:
                operation = await client.call_tool(
                    "edit",
                    {"path": "session.json", "oldText": initial, "newText": updated},
                )
            if operation.is_error:
                raise ValueError("MCP file operation failed")
            result = await client.call_tool(
                "bash", {"command": "python3 /recipe/publish.py"}
            )
            if result.is_error:
                raise ValueError("publisher failed")
            editions.append(json.loads(result.content[0].text))
        index = await client.call_tool("read", {"path": "layout/index.json"})
        assert json.loads(index.content[0].text) == json.loads(
            (work / "layout/index.json").read_text()
        )
    return names, editions


def main(client_home):
    ap = argparse.ArgumentParser(description=__doc__)
    for arg in ("computer", "python-dir", "book-dir", "output", "oras"):
        ap.add_argument("--" + arg, required=True, type=Path)
    a = ap.parse_args()
    out = a.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    docker = client_home / ".docker"
    docker.mkdir()
    (docker / "config.json").write_text('{"auths":{}}')
    os.environ["DOCKER_CONFIG"] = str(docker)
    os.environ["HOME"] = str(client_home)
    os.environ["XDG_CONFIG_HOME"] = str(client_home)
    work = out / "work"
    work.mkdir()
    names, editions = asyncio.run(build(a, out, work))
    assert (
        editions[0]["book"] == editions[1]["book"] != editions[0]["calendar"]
        and editions[0]["calendar"] != editions[1]["calendar"]
    )
    layout = work / "layout"
    origin = (a.book_dir / "four-moons.epub").read_bytes()
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
    address = "127.0.0.1:" + str(port)
    with (out / "registry.log").open("w") as log:
        proc = subprocess.Popen(
            ["crane", "registry", "serve", "--address", address], stdout=log, stderr=log
        )
        try:
            for _ in range(100):
                if proc.poll() is not None:
                    raise RuntimeError("registry stopped before startup")
                try:
                    with urllib.request.urlopen(
                        "http://" + address + "/v2/", timeout=1
                    ):
                        break
                except OSError:
                    time.sleep(0.05)
            else:
                raise RuntimeError("registry did not become ready")
            received = []
            for row in editions:
                tag = row["edition"]
                remote = address + "/library/reading-night:" + tag
                run(
                    a.oras,
                    "cp",
                    "--from-oci-layout",
                    str(layout) + ":" + tag,
                    "--to-plain-http",
                    remote,
                )
                manifest = json.loads(run("crane", "manifest", "--insecure", remote))
                assert [d["digest"] for d in manifest["layers"]] == [
                    row["book"],
                    row["calendar"],
                ]
                observed = run("crane", "digest", "--insecure", remote).strip()
                assert observed == row["manifest"]
                folder = out / (tag + "-download")
                run(a.oras, "pull", "--plain-http", remote, "--output", folder)
                epub = (folder / "four-moons.epub").read_bytes()
                ics = (folder / "reading-room.ics").read_bytes()
                assert epub == origin and sha(ics) == row["calendar"]
                assert (
                    b"DTSTART:20261124T"
                    + (b"173000Z" if tag == "original" else b"180000Z")
                    in ics
                )
                with zipfile.ZipFile(folder / "four-moons.epub") as b:
                    assert b.read("mimetype") == b"application/epub+zip"
                received.append(
                    {
                        **row,
                        "bytes_epub": len(epub),
                        "bytes_calendar": len(ics),
                        "registry_manifest_digest": observed,
                    }
                )
            tags = sorted(
                run(
                    "crane", "ls", "--insecure", address + "/library/reading-night"
                ).split()
            )
            assert tags == ["amended", "original"]
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
    receipt = {
        "mcp_tools": names,
        "registry": "loopback crane registry serve",
        "tags": tags,
        "editions": received,
        "physical_blobs": len(list((layout / "blobs/sha256").iterdir())),
        "distinct_book_layers": len({r["book"] for r in received}),
    }
    (out / "checks.json").write_text(json.dumps(receipt, indent=2) + "\n")
    subprocess.run(
        [
            a.computer.resolve(),
            "-mount",
            str(out) + ":/work",
            "-mount",
            str(out) + ":/deliveries:ro",
            "-mount",
            str(HERE / "recipe") + ":/recipe:ro",
            "-python",
            a.python_dir.resolve() / "python.wasm",
            "-python-lib",
            a.python_dir.resolve() / "lib",
            "-c",
            "python3 /recipe/program.py",
        ],
        check=True,
    )
    print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as home:
        main(Path(home))
