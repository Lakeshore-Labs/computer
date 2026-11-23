"""Read the 6,399 teaching-model samples from the pinned big-endian elevation grid."""

import array
import gzip
import hashlib
import json
import math
import sys
from pathlib import Path

inputs = Path("/source")
receipt = json.loads(Path("/recipe/source.json").read_text())
compressed = (inputs / "oslo.hgt.gz").read_bytes()
if hashlib.sha256(compressed).hexdigest() != receipt["gzip_sha256"]:
    raise ValueError("terrain tile differs from the source receipt")
data = gzip.decompress(compressed)
side = math.isqrt(len(data) // 2)
if side * side * 2 != len(data):
    raise ValueError("invalid SRTM sample grid")
heights = array.array("h")
heights.frombytes(data)
if sys.byteorder == "little":
    heights.byteswap()
west, south, east, north = 10.57, 59.84, 10.86, 59.997
nx, ny = 81, 79
grid = []
for y in range(ny):
    lat = south + (north - south) * y / (ny - 1)
    row = round((60 - lat) * (side - 1))
    line = []
    for x in range(nx):
        lon = west + (east - west) * x / (nx - 1)
        col = round((lon - 10) * (side - 1))
        h = heights[row * side + col]
        if h == -32768:
            raise ValueError("the selected model contains missing elevations")
        line.append(h)
    grid.append(line)

Path("grid.json").write_text(
    json.dumps(
        {"bounds": [west, south, east, north], "metres": grid}, separators=(",", ":")
    )
    + "\n"
)
