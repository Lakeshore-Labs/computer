"""Write closed, millimetre-scale STL models from the captured elevation grid."""

import hashlib
import json
import math
import struct
from pathlib import Path

grid_file = json.loads(Path("/source/grid.json").read_text())
west, south, east, north = grid_file["bounds"]
grid = grid_file["metres"]
ny = len(grid)
nx = len(grid[0])
if ny < 2 or nx < 2 or any(len(row) != nx for row in grid):
    raise ValueError("terrain samples must form a rectangular grid")
width = 110.0
earth_radius = 6371000
mid = math.radians((south + north) / 2)
ground_width = earth_radius * math.cos(mid) * math.radians(east - west)
ground_depth = earth_radius * math.radians(north - south)
mm_per_m = width / ground_width
depth = ground_depth * mm_per_m
base = 2.5


def normal(a, b, c):
    u = [b[i] - a[i] for i in range(3)]
    v = [c[i] - a[i] for i in range(3)]
    n = [
        u[1] * v[2] - u[2] * v[1],
        u[2] * v[0] - u[0] * v[2],
        u[0] * v[1] - u[1] * v[0],
    ]
    length = math.sqrt(sum(x * x for x in n))
    return [x / length for x in n]


def make(factor):
    verts = [
        (
            x * width / (nx - 1),
            y * depth / (ny - 1),
            base + max(0, grid[y][x]) * mm_per_m * factor,
        )
        for y in range(ny)
        for x in range(nx)
    ]
    faces = []
    for y in range(ny - 1):
        for x in range(nx - 1):
            a = y * nx + x
            b = a + 1
            d = a + nx
            c = d + 1
            faces.extend(((a, b, c), (a, c, d)))
    ring = [
        *range(nx),
        *(y * nx + nx - 1 for y in range(1, ny)),
        *((ny - 1) * nx + x for x in range(nx - 2, -1, -1)),
        *(y * nx for y in range(ny - 2, 0, -1)),
    ]
    bottom = []
    for i in ring:
        x, y, _ = verts[i]
        bottom.append(len(verts))
        verts.append((x, y, 0.0))
    center = len(verts)
    verts.append((width / 2, depth / 2, 0.0))
    for i, top in enumerate(ring):
        j = (i + 1) % len(ring)
        lo = bottom[i]
        nextlo = bottom[j]
        faces.extend(((lo, nextlo, ring[j]), (lo, ring[j], top), (center, nextlo, lo)))
    path = Path(f"oslo-{factor}x.stl")
    with path.open("wb") as f:
        f.write(
            f"Oslo teaching relief: millimetres; vertical exaggeration {factor}x".encode().ljust(
                80, b" "
            )
        )
        f.write(struct.pack("<I", len(faces)))
        for i, j, k in faces:
            a, b, c = verts[i], verts[j], verts[k]
            f.write(struct.pack("<12fH", *normal(a, b, c), *a, *b, *c, 0))
    return {
        "file": path.name,
        "vertical_exaggeration": factor,
        "triangles": len(faces),
        "unique_vertices": len(verts),
        "width_mm": width,
        "depth_mm": depth,
        "height_mm": max(v[2] for v in verts),
        "base_mm": base,
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
    }


meta = {
    "source": "Mapzen Terrain Tiles, Norway terrain © Kartverket. Europe terrain uses Copernicus data funded by the European Union, EU-DEM layers.",
    "bounds": [west, south, east, north],
    "grid": [nx, ny],
    "source_height_range_m": [min(map(min, grid)), max(map(max, grid))],
    "below_zero_clamped": True,
    "models": [make(1), make(4)],
}
Path("models.json").write_text(json.dumps(meta, indent=2, ensure_ascii=False) + "\n")
print(json.dumps(meta["models"]))
