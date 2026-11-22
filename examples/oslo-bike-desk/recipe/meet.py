"""Find bikes for a group by reading actual projected point geometries in the GeoPackage."""

import json
import math
import sqlite3
import struct
from pathlib import Path


def point(blob):
    if blob[:3] != b"GP\x00":
        raise ValueError("expected GeoPackage v1 binary geometry")
    flags = blob[3]
    endian = "<" if flags & 1 else ">"
    (srs,) = struct.unpack_from(endian + "i", blob, 4)
    if srs != 25832 or flags & 0x30:
        raise ValueError("expected a nonempty standard ETRS89 / UTM 32N point")
    envelope = (flags >> 1) & 7
    offset = 8 + 8 * {0: 0, 1: 4, 2: 6, 3: 6, 4: 8}[envelope]
    wkb_order = "<" if blob[offset] == 1 else ">"
    kind, x, y = struct.unpack_from(wkb_order + "Idd", blob, offset + 1)
    if kind != 1:
        raise ValueError("only 2D stations are supported")
    return x, y


with sqlite3.connect("file:/work/stations.gpkg?mode=ro", uri=True) as conn:
    conn.execute("PRAGMA temp_store=MEMORY")
    fields = conn.execute(
        "select column_name,srs_id from gpkg_geometry_columns where table_name='stations'"
    ).fetchone()
    if fields != ("geom", 25832):
        raise ValueError("unexpected GeoPackage schema")
    rows = conn.execute(
        "select ID, NAME, BIKES, DOCKS, RENTING, INSTALLED, geom, FEED_UTC from stations"
    ).fetchall()
places = [
    (i, name, bikes, docks, renting, installed, point(geom), feed)
    for i, name, bikes, docks, renting, installed, geom, feed in rows
]
start = next(row for row in places if row[0] == "478")
options = []
for i, name, bikes, docks, renting, installed, xy, feed in places:
    if installed and renting and bikes:
        options.append(
            {
                "id": i,
                "name": name,
                "bikes": bikes,
                "distance_m": round(math.dist(start[6], xy), 3),
                "east_m": round(xy[0] - start[6][0], 3),
                "north_m": round(xy[1] - start[6][1], 3),
            }
        )
options.sort(key=lambda option: (option["distance_m"], option["id"]))
required = 10
eligible = [o for o in options if o["bikes"] >= required]
report = {
    "from": start[1],
    "starting_bikes": start[2],
    "group_size": required,
    "feed_utc": start[7],
    "total_features": len(places),
    "eligible_stations": len(eligible),
    "nearest": eligible[:3],
    "distance": "straight-line metres in EPSG:25832",
}
Path("meeting.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
payload = {"report": report, "renting": options}
encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).replace(
    "<", "\\u003c"
)
Path("meeting.html").write_text(
    Path("/recipe/meeting.html").read_text().replace("{{GROUP}}", encoded)
)
print(json.dumps(report, ensure_ascii=False))
