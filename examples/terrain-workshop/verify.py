"""Read binary STL independently and check topology, winding and physical dimensions."""

import hashlib
import math
import struct
from collections import Counter, defaultdict
from pathlib import Path


def inspect(file: Path):
    data = file.read_bytes()
    count = struct.unpack_from("<I", data, 80)[0]
    if len(data) != 84 + 50 * count:
        raise ValueError("unexpected STL length")
    edges = Counter()
    winding = Counter()
    adjacent = defaultdict(set)
    volume = 0.0
    for facet in struct.iter_unpack("<12fH", data[84:]):
        a, b, c = facet[3:6], facet[6:9], facet[9:12]
        if len({a, b, c}) != 3 or not all(map(math.isfinite, (*a, *b, *c))):
            raise ValueError("invalid triangle")
        volume += (
            a[0] * (b[1] * c[2] - b[2] * c[1])
            + a[1] * (b[2] * c[0] - b[0] * c[2])
            + a[2] * (b[0] * c[1] - b[1] * c[0])
        ) / 6
        for start, end in ((a, b), (b, c), (c, a)):
            edge = tuple(sorted((start, end)))
            edges[edge] += 1
            winding[edge] += 1 if start < end else -1
            adjacent[start].add(end)
            adjacent[end].add(start)
    if set(edges.values()) != {2} or any(winding.values()) or volume <= 0:
        raise ValueError("surface is open or orientations disagree")
    pending = [next(iter(adjacent))]
    visited = set()
    while pending:
        vertex = pending.pop()
        if vertex not in visited:
            visited.add(vertex)
            pending.extend(adjacent[vertex] - visited)
    if len(visited) != len(adjacent):
        raise ValueError("multiple disconnected components")
    bounds = list(zip(*adjacent))
    return {
        "sha256": hashlib.sha256(data).hexdigest(),
        "triangles": count,
        "vertices": len(adjacent),
        "edges_with_two_opposite_faces": len(edges),
        "components": 1,
        "dimensions_mm": [round(max(axis) - min(axis), 5) for axis in bounds],
        "signed_volume_mm3": round(volume, 3),
    }
