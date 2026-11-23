"""Save one standard MCP resource from the field guide to a new local file."""

import argparse
import asyncio
import base64
import hashlib
from pathlib import Path

from replay import connect


async def fetch(guide, uri, output):
    async with connect(guide) as session:
        result = await session.read_resource(uri)
        if len(result.contents) != 1 or str(result.contents[0].uri) != uri:
            raise ValueError(
                "The response did not contain the requested single resource."
            )
        content = result.contents[0]
        data = (
            content.text.encode("utf-8")
            if hasattr(content, "text")
            else base64.b64decode(content.blob, validate=True)
        )
    with output.open("xb") as dest:
        dest.write(data)
    print(
        f"{output.name}  {len(data):,} bytes\nSHA-256  {hashlib.sha256(data).hexdigest()}"
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--guide", required=True, type=Path)
    parser.add_argument("uri")
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    asyncio.run(fetch(args.guide, args.uri, args.output))
