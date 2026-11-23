"""Compare two actual MCP result paths by forwarding each result to the same Gemini prompt."""

import argparse
import asyncio
import csv
import datetime as dt
import hashlib
import io
import json
import os
from pathlib import Path
from tempfile import TemporaryDirectory
from urllib import request, error

from mcp import ClientSession, StdioServerParameters, stdio_client

MODEL = "gemini-3.5-flash-lite"
QUESTION = (
    "The records below are CSV with columns trip,route,riders. "
    "Find the last row whose route is Pier. Return just its trip and riders as two comma-separated integers.\n\n"
)
FILTER = """set -eo pipefail
timetable read-text-file --path trips.csv | jq -r '.content' | awk -F, 'NR==1 {header=$0} $2=="Pier" {last=$0} END {print header; print last}'"""


def contents(count):
    buffer = io.StringIO()
    writer = csv.writer(buffer, lineterminator="\n")
    writer.writerow(("trip", "route", "riders"))
    expected = None
    for i in range(1, count + 1):
        route = ("Pier", "Museum", "North")[(i - 1) % 3]
        riders = 12 + (i * 17) % 53
        writer.writerow((i, route, riders))
        if route == "Pier":
            expected = (i, riders)
    return buffer.getvalue(), expected


async def mcp_result(server, tool, arguments):
    async with (
        stdio_client(StdioServerParameters(**server)) as (reader, writer),
        ClientSession(reader, writer) as client,
    ):
        await client.initialize()
        outcome = await client.call_tool(tool, arguments)
        if outcome.is_error:
            raise RuntimeError(f"{tool} failed.")
        if tool == "read_text_file":
            return outcome.structured_content["content"]
        return "".join(block.text for block in outcome.content if block.type == "text")


def google(method, body):
    url = f"https://generativelanguage.googleapis.com/v1beta/models/{MODEL}:{method}"
    req = request.Request(
        url,
        data=json.dumps(body).encode(),
        method="POST",
        headers={
            "Content-Type": "application/json",
            "x-goog-api-key": os.environ["GEMINI_API_KEY"],
        },
    )
    try:
        with request.urlopen(req, timeout=65) as response:
            return json.load(response)
    except error.HTTPError as e:
        raise RuntimeError(f"{method} returned HTTP {e.code}") from None


async def main(args):
    if "GEMINI_API_KEY" not in os.environ:
        raise SystemExit("Set GEMINI_API_KEY before requesting live measurements.")
    if (args.output / "measurement.json").exists():
        raise SystemExit("Choose a fresh output directory for a new measurement.")
    args.output.mkdir(parents=True, exist_ok=True)
    fs = str(args.filesystem.resolve())
    cli = str(args.computer.resolve())
    measurements = []
    with TemporaryDirectory() as temp:
        top = Path(temp)
        src = top / "upstream"
        src.mkdir()
        work = top / "work"
        work.mkdir()
        upstream = {"command": fs, "args": ["--read-only-dir", str(src)]}
        config = top / "computer.json"
        config.write_text(
            json.dumps(
                {
                    "mounts": [str(work) + ":/work"],
                    "mcpServers": {"timetable": upstream},
                }
            )
        )
        computer = {"command": cli, "args": ["-config", str(config), "mcp"]}
        for n in [72, 720, 1440]:
            csv_text, expected = contents(n)
            (src / "trips.csv").write_text(csv_text)
            full = await mcp_result(upstream, "read_text_file", {"path": "trips.csv"})
            selected = await mcp_result(computer, "bash", {"command": FILTER})
            if full != csv_text:
                raise ValueError("The direct MCP read differed from the source.")
            short_rows = list(csv.reader(io.StringIO(selected)))
            if short_rows != [
                ["trip", "route", "riders"],
                [str(expected[0]), "Pier", str(expected[1])],
            ]:
                raise ValueError(
                    "The Computer result differed from the independent lookup: "
                    + repr(selected[:160])
                )
            results = {}
            for label, body in [("direct", full), ("computer", selected)]:
                prompt = QUESTION + body
                payload = {"contents": [{"role": "user", "parts": [{"text": prompt}]}]}
                count = google("countTokens", payload)["totalTokens"]
                res = {
                    "forwarded_utf8_bytes": len(body.encode()),
                    "count_tokens": count,
                }
                if n == 1440:
                    answer = google(
                        "generateContent",
                        {
                            **payload,
                            "generationConfig": {
                                "temperature": 0,
                                "maxOutputTokens": 512,
                            },
                        },
                    )
                    parts = answer["candidates"][0]["content"]["parts"]
                    reply = "".join(
                        p["text"]
                        for p in parts
                        if p.get("text") and not p.get("thought")
                    ).strip()
                    if reply.replace(" ", "") != ",".join(map(str, expected)):
                        raise ValueError(
                            "The model answer differed from the expected last row: "
                            + reply
                        )
                    res.update(
                        answer=reply,
                        usage=answer["usageMetadata"],
                        model_version=answer.get("modelVersion"),
                        checked=True,
                    )
                results[label] = res
            row = dict(
                rows=n,
                expected=expected,
                sha256=hashlib.sha256(csv_text.encode()).hexdigest(),
                results=results,
            )
            measurements.append(row)
            (args.output / "measurement.json").write_text(
                json.dumps(
                    dict(
                        measured_at=dt.datetime.now(dt.timezone.utc).isoformat(),
                        model=MODEL,
                        question=QUESTION,
                        filter=FILTER,
                        cases=measurements,
                    ),
                    indent=2,
                )
                + "\n"
            )
            with (args.output / "tokens.csv").open("w", newline="") as chart_file:
                writer = csv.writer(chart_file, lineterminator="\n")
                writer.writerow(("trips", "full_file", "filtered_row"))
                for case in measurements:
                    writer.writerow(
                        (
                            case["rows"],
                            case["results"]["direct"]["count_tokens"],
                            case["results"]["computer"]["count_tokens"],
                        )
                    )
            print(
                n,
                results["direct"]["count_tokens"],
                results["computer"]["count_tokens"],
                flush=True,
            )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--filesystem", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    asyncio.run(main(parser.parse_args()))
