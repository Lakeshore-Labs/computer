"""Reverse the TIFF predictor, crop three real RGB windows and write lossless PNGs."""

import hashlib
import json
import struct
import zlib
from pathlib import Path

plan = json.loads(Path("plan.json").read_bytes())
tiles = {}
tw, th, cols = (plan[k] for k in ("tile_width", "tile_height", "tile_columns"))
for i, r in plan["tiles"].items():
    data = Path(f"block-{i}.deflate").read_bytes()
    if len(data) != r["size"]:
        raise ValueError("range length differs from TIFF byte count")
    raw = bytearray(zlib.decompress(data))
    if len(raw) != tw * th * 3:
        raise ValueError("unexpected uncompressed block size")
    for row in range(th):
        base = row * tw * 3
        for p in range(base + 3, base + tw * 3):
            raw[p] = (raw[p] + raw[p - 3]) & 255
    tiles[int(i)] = raw
    r["sha256"] = hashlib.sha256(data).hexdigest()


def chunk(kind, data):
    return (
        struct.pack(">I", len(data))
        + kind
        + data
        + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    )


for w in plan["windows"]:
    pixels = bytearray()
    for y in range(w["y"], w["y"] + w["height"]):
        x = w["x"]
        end = x + w["width"]
        while x < end:
            tid = y // th * cols + x // tw
            n = min(end - x, tw - x % tw)
            j = ((y % th) * tw + x % tw) * 3
            pixels.extend(tiles[tid][j : j + 3 * n])
            x += n
    w["rgb_sha256"] = hashlib.sha256(pixels).hexdigest()
    stride = w["width"] * 3
    packed = b"".join(
        b"\x00" + pixels[i : i + stride] for i in range(0, len(pixels), stride)
    )
    png = (
        b"\x89PNG\r\n\x1a\n"
        + chunk(
            b"IHDR", struct.pack(">IIBBBBB", w["width"], w["height"], 8, 2, 0, 0, 0)
        )
        + chunk(b"IDAT", zlib.compress(packed))
        + chunk(b"IEND", b"")
    )
    Path(w["id"] + ".png").write_bytes(png)
    w["png_sha256"] = hashlib.sha256(png).hexdigest()
plan["image_range_bytes"] = len(Path("header.bin").read_bytes()) + sum(
    r["size"] for r in plan["tiles"].values()
)
plan["full_cog_bytes"] = int(
    json.loads(Path("/source/probe.json").read_bytes())["content_range"].split("/")[-1]
)
Path("windows.json").write_text(json.dumps(plan, indent=2, ensure_ascii=False) + "\n")
print(
    plan["image_range_bytes"],
    "image bytes requested",
    len(plan["tiles"]),
    "compressed blocks",
)
