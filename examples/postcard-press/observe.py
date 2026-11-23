"""Use an independent Python MCP session to read back and render the browser's postcard."""

import argparse, asyncio, base64, hashlib, json
from pathlib import Path
import subprocess
import xml.etree.ElementTree as ET
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client


async def observe(args):
    async with streamable_http_client(args.endpoint.rstrip("/") + "/mcp") as streams:
        async with ClientSession(streams[0], streams[1]) as client:
            await client.initialize()
            names = sorted(t.name for t in (await client.list_tools()).tools)
            assert names == ["bash", "edit", "read", "write"]

            async def read(name):
                r = await client.call_tool("read", {"path": "/desk/" + name})
                assert not r.is_error and r.content[0].type == "text", name
                return r.content[0].text

            svg = await read("postcard.svg")
            receipt = json.loads(await read("receipt.json"))
            order = json.loads(await read("order.json"))
    data = svg.encode()
    sha = lambda b: hashlib.sha256(b).hexdigest()
    assert sha(data) == receipt["svg_sha256"] and len(data) == receipt["svg_bytes"], (
        len(data),
        receipt["svg_bytes"],
    )
    assert order == {"museum": receipt["museum"], "inscription": receipt["inscription"]}
    original = (args.collection / order["museum"] / "painting.jpg").read_bytes()
    assert sha(original) == receipt["source_jpeg_sha256"]
    xml = ET.fromstring(data)
    assert xml.attrib["width"] == "148mm" and xml.attrib["height"] == "105mm"
    href = xml.find("{*}image").attrib["{http://www.w3.org/1999/xlink}href"]
    jpg = base64.b64decode(href.split(",", 1)[1])
    assert sha(jpg) == receipt["thumbnail_sha256"]
    texts = [t.text for t in xml.findall("{*}text")]
    assert receipt["inscription"] in texts and receipt["title"] in texts
    args.output.mkdir(parents=True, exist_ok=False)
    (args.output / "postcard.svg").write_bytes(data)
    subprocess.run(
        [
            "rsvg-convert",
            "-w",
            "888",
            "-h",
            "630",
            "-o",
            str(args.output / "postcard.png"),
            str(args.output / "postcard.svg"),
        ],
        check=True,
    )
    if args.download:
        assert args.download.read_bytes() == data
    proof = {
        "tool_names": names,
        "order": order,
        "guest": receipt,
        "independent_svg_sha256": sha(data),
        "original_museum_jpeg_matches": True,
        "embedded_jpeg_matches": True,
        "native_renderer": "librsvg",
        "raster_sha256": sha((args.output / "postcard.png").read_bytes()),
        "browser_download_matched": bool(args.download),
    }
    (args.output / "receipt.json").write_text(
        json.dumps(proof, indent=2, ensure_ascii=False) + "\n"
    )
    print(receipt["title"], len(data), sha(data))


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--endpoint", required=True)
    p.add_argument("--output", type=Path, required=True)
    p.add_argument("--download", type=Path)
    p.add_argument(
        "--collection",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "museum-study" / "sources",
    )
    asyncio.run(observe(p.parse_args()))
