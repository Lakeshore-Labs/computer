"""Measure what passes through Computer's outer MCP connection during a report."""
import argparse
import asyncio
import base64
import csv
import hashlib
import json
from pathlib import Path
import struct
from tempfile import TemporaryDirectory

from mcp import ClientSession, StdioServerParameters, stdio_client


def make_input(directory):
    source = directory / "trips.csv"
    totals = {}
    routes = ("Pier", "Museum", "North")
    minimum_riders = {"Pier": 38, "Museum": 22, "North": 12}
    count = 50_000
    with source.open("w", newline="") as stream:
        writer = csv.writer(stream, lineterminator="\n")
        writer.writerow(("trip", "route", "riders"))
        for i in range(count):
            route = routes[i % len(routes)]
            riders = minimum_riders[route] + (i * 17) % 31
            writer.writerow((i + 1, route, riders))
            totals[route] = totals.get(route, 0) + riders
    return source, count, totals


async def measure_report(client, work, source, count, expected, events):
    command = Path(__file__).with_name("compute.sh").read_text().strip()
    result = await client.call_tool("bash", {"command": command, "timeout_ms": 120_000})
    summary = "".join(block.text for block in result.content if block.type == "text")
    if result.is_error or f"{source.stat().st_size} bytes, {count} trips" not in summary:
        raise RuntimeError(f"Computer did not report the expected input: {summary}")

    with (work / "totals.csv").open(newline="") as stream:
        totals = {row["route"]: int(row["boardings"]) for row in csv.DictReader(stream)}
    if totals != expected:
        raise ValueError(f"Route totals differ from the fixture: {totals}")
    events.append({"method": "tools/call", "tool": "bash", "returned_text": summary,
                   "result_text_bytes": len(summary.encode())})

    result = await client.call_tool("read", {"path": "report.png"})
    images = [block for block in result.content if block.type == "image"]
    if result.is_error or len(images) != 1:
        raise ValueError("The MCP read did not return one image.")
    block = images[0]
    data = base64.b64decode(block.data, validate=True)
    png = b"\x89PNG\r\n\x1a\n"
    if block.mime_type != "image/png" or data != (work / "report.png").read_bytes() or not data.startswith(png):
        raise ValueError("The returned PNG differs from the mounted chart.")
    size = struct.unpack(">II", data[16:24])
    if size != (800, 500):
        raise ValueError(f"Unexpected chart size: {size}")
    logo = (work / "source-logo.png").read_bytes()
    if not logo.startswith(png) or struct.unpack(">II", logo[16:24]) != (20, 20):
        raise ValueError("The second upstream tool did not supply its 20 x 20 image.")
    events.append({"method": "tools/call", "tool": "read", "path": "report.png",
                   "content_type": block.mime_type, "dimensions": size, "bytes": len(data),
                   "sha256": hashlib.sha256(data).hexdigest()})
    return totals, data, len(logo)


async def main(args):
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    products = ("trace.json", "totals.csv", "report.png")
    if any((output / name).exists() for name in products):
        raise FileExistsError("Choose an output directory without an earlier report.")
    with TemporaryDirectory() as temp:
        root = Path(temp)
        upstream = root / "upstream-only"
        upstream.mkdir()
        work = root / "mounted-output"
        work.mkdir()
        source, count, expected = make_input(upstream)
        events = []
        trace = {"input_bytes": source.stat().st_size, "input_rows": count,
                 "input_sha256": hashlib.sha256(source.read_bytes()).hexdigest(), "events": events}
        servers = {
            "source": {"command": str((args.server_bin / "mcp-server-filesystem").resolve()),
                       "args": ["--read-only-dir", str(upstream)]},
            "reference": {"command": str((args.server_bin / "mcp-server-everything").resolve()),
                          "args": ["stdio"]},
        }
        for connected in ({}, servers):
            config = root / "computer.json"
            config.write_text(json.dumps({"mounts": [f"{work}:/work"], "mcpServers": connected}))
            runtime = args.python_wasi.resolve()
            server = StdioServerParameters(command=str(args.computer.resolve()), args=[
                "-config", str(config), "-python", str(runtime / "python.wasm"),
                "-python-lib", str(runtime / "lib"), "mcp"])
            async with stdio_client(server) as (reader, writer), ClientSession(reader, writer) as client:
                await client.initialize()
                names = sorted(tool.name for tool in (await client.list_tools()).tools)
                if names != ["bash", "edit", "read", "write"]:
                    raise ValueError(f"Unexpected outer interface: {names}")
                events.append({"method": "tools/list", "upstream_servers": len(connected), "tools": names})
                if connected:
                    totals, chart, logo_size = await measure_report(client, work, source, count, expected, events)
        trace.update(pass_checks=True, totals=totals, source_logo_bytes=logo_size)
        (output / "report.png").write_bytes(chart)
        (output / "totals.csv").write_bytes((work / "totals.csv").read_bytes())
        (output / "trace.json").write_text(json.dumps(trace, indent=2) + "\n")
        print(events[2]["returned_text"], end="")
        print(f'MCP bash returned {events[2]["result_text_bytes"]} text bytes. MCP read returned {len(chart)} image bytes.')


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--python-wasi", type=Path, required=True)
    parser.add_argument("--server-bin", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    asyncio.run(main(parser.parse_args()))
