"""Write, edit and read the same mounted file through three independent MCP clients."""
import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory

from mcp import ClientSession, StdioServerParameters, stdio_client

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--computer", type=Path, required=True)
parser.add_argument("--output", type=Path, required=True)
args = parser.parse_args()
output = args.output.resolve()
if (output / "handoff.txt").exists():
    parser.error("Use a fresh output directory so an existing handoff.txt is preserved.")
output.mkdir(parents=True, exist_ok=True)
server = {"command": str(args.computer.resolve()), "args": ["-mount", f"{output}:/work", "mcp"]}
expected = "Evening ferry: Pier"

async def verify():
    async with stdio_client(StdioServerParameters(**server)) as (reader, writer):
        async with ClientSession(reader, writer) as client:
            await client.initialize()
            result = await client.call_tool("read", {"path": "handoff.txt"})
            content = "".join(block.text for block in result.content if block.type == "text")
            if result.is_error or content != expected:
                raise RuntimeError(f"Unexpected MCP read-back: {content!r}")
            result = await client.call_tool("bash", {"command": "sha256sum handoff.txt"})
            checksum = hashlib.sha256(expected.encode()).hexdigest()
            if result.is_error or not result.content[0].text.startswith(checksum):
                raise RuntimeError("The shell checksum did not match the text read by the SDK.")
            print("Python MCP SDK read:", content)
            print("Verified SHA-256:", checksum)

with TemporaryDirectory() as temp:
    config = Path(temp) / "servers.json"
    config.write_text(json.dumps({"mcpServers": {"computer": server}}))
    inspector = ["npx", "--yes", "--package=@modelcontextprotocol/inspector@2.13.0", "mcp-inspector"]
    porter = ["npx", "--yes", "--package=mcporter@0.17.1", "mcporter"]
    subprocess.run([*inspector, "--cli", "--config", str(config), "--server", "computer",
                    "--method", "tools/call", "--tool-name", "write", "--tool-arg",
                    "path=handoff.txt", "content=Morning ferry: Pier"], check=True,
                   env={**os.environ, "MCP_INSPECTOR_SECRET_STORE": "memory"})
    subprocess.run([*porter, "--config", str(config), "call", "computer.edit", "--args",
                    json.dumps({"path": "handoff.txt", "oldText": "Morning", "newText": "Evening"}),
                    "--output", "json"], check=True)
    asyncio.run(verify())
if (output / "handoff.txt").read_bytes() != expected.encode():
    raise RuntimeError("The mounted host file differs from the MCP read-back.")
