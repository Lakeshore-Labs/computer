"""Read a small TIFF directory and plan only the full-resolution blocks each window needs."""

import json
import struct
from pathlib import Path

data = Path("header.bin").read_bytes()
if data[:4] != b"II*\x00":
    raise ValueError("expected a little-endian classic TIFF")
(first,) = struct.unpack_from("<I", data, 4)
(count,) = struct.unpack_from("<H", data, first)
tags = {}
for i in range(count):
    pos = first + 2 + 12 * i
    tag, kind, number, offset = struct.unpack_from("<HHII", data, pos)
    if kind not in (3, 4) or tag not in (
        256,
        257,
        258,
        259,
        262,
        277,
        284,
        317,
        322,
        323,
        324,
        325,
    ):
        continue
    size, fmt = (2, "H") if kind == 3 else (4, "I")
    at = pos + 8 if number * size <= 4 else offset
    tags[tag] = struct.unpack_from("<" + str(number) + fmt, data, at)
for k, expected in {
    258: (8, 8, 8),
    259: (8,),
    262: (2,),
    277: (3,),
    284: (1,),
    317: (2,),
}.items():
    if tags[k] != expected:
        raise ValueError(
            "this reader expects chunky unsigned RGB, Deflate and horizontal prediction"
        )
width, height = tags[256][0], tags[257][0]
tw, th = tags[322][0], tags[323][0]
columns = (width + tw - 1) // tw
spec = json.loads(Path("/recipe/windows.json").read_text())
item = json.loads(Path("/source/item.json").read_bytes())
asset = item["assets"]["visual"]
if (
    asset["proj:shape"] != [height, width]
    or item["properties"]["proj:epsg"] != 32632
    or asset["proj:transform"][:6]
    != [
        spec["pixel_m"],
        0,
        spec["origin_east_m"],
        0,
        -spec["pixel_m"],
        spec["origin_north_m"],
    ]
):
    raise ValueError("published STAC grid differs from the capture")
windows = []
needed = set()
for site in spec["locations"]:
    # The reference grid and each centre were independently checked with PROJ.
    top_left_x = int((site["east_m"] - spec["origin_east_m"]) / spec["pixel_m"]) - 128
    top_left_y = int((spec["origin_north_m"] - site["north_m"]) / spec["pixel_m"]) - 128
    if not (0 <= top_left_x <= width - 256 and 0 <= top_left_y <= height - 256):
        raise ValueError("window outside image")
    ids = []
    for row in range(top_left_y // th, (top_left_y + 255) // th + 1):
        for col in range(top_left_x // tw, (top_left_x + 255) // tw + 1):
            ids.append(row * columns + col)
    needed.update(ids)
    windows.append(
        {
            **site,
            "x": top_left_x,
            "y": top_left_y,
            "width": 256,
            "height": 256,
            "tiles": ids,
        }
    )
tiles = {str(i): {"start": tags[324][i], "size": tags[325][i]} for i in sorted(needed)}
Path("plan.json").write_text(
    json.dumps(
        {
            "width": width,
            "height": height,
            "tile_width": tw,
            "tile_height": th,
            "tile_columns": columns,
            "windows": windows,
            "tiles": tiles,
        },
        indent=2,
        ensure_ascii=False,
    )
    + "\n"
)
Path("ranges.tsv").write_text(
    "".join(
        f"{i}\t{r['start']}\t{r['start'] + r['size'] - 1}\n" for i, r in tiles.items()
    )
)
print(json.dumps(tiles))
