"""Create a station shapefile and an offline Leaflet reader inside Computer."""

import base64
import datetime as dt
import hashlib
import json
import sys
from html import escape
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo

SOURCE = Path("/source")
STATIC = Path("/static")
VENDOR = Path("/vendor")
sys.path.insert(0, str(next(VENDOR.glob("pyshp-*.whl"))))
import shapefile


def encoded(data):
    return base64.b64encode(data).decode("ascii")


def iso(epoch):
    return dt.datetime.fromtimestamp(epoch, dt.UTC).strftime("%Y-%m-%d %H:%M UTC")


info = json.loads((SOURCE / "station_information.json").read_bytes())
stations = {str(s["station_id"]): s for s in info["data"]["stations"]}
history = []
for f in sorted(SOURCE.glob("2*/station_status.json")):
    if (
        not (f.parent / "receipt.json").exists()
        or json.loads((f.parent / "receipt.json").read_bytes()).get("guest_exit") != 0
    ):
        continue
    record = json.loads(f.read_bytes())
    stats = {
        str(s["station_id"]): [
            s["num_bikes_available"],
            s["num_docks_available"],
            int(s["is_renting"]),
            int(s["is_returning"]),
        ]
        for s in record["data"]["stations"]
        if s["is_installed"]
    }
    history.append(
        {
            "source_time": iso(record["last_updated"]),
            "collected": f.parent.name,
            "stations": stats,
        }
    )
if not history:
    raise RuntimeError("no completed GBFS round")
latest = history[-1]
ids = sorted(stations)
with shapefile.Writer("oslo", shapeType=shapefile.POINT, encoding="utf-8") as shapes:
    for name, kind, size in [
        ("ID", "C", 16),
        ("NAME", "C", 120),
        ("BIKES", "N", 5),
        ("DOCKS", "N", 5),
        ("RENTING", "N", 1),
        ("RETURNING", "N", 1),
        ("INSTALLED", "N", 1),
        ("FEED_UTC", "C", 24),
    ]:
        shapes.field(name, kind, size)
    for id in ids:
        s = stations[id]
        shapes.point(s["lon"], s["lat"])
        status = latest["stations"].get(id)
        shapes.record(
            id,
            s["name"],
            *(status or (None, None, 0, 0)),
            int(status is not None),
            latest["source_time"],
        )
# DBF header bytes 1–3 are the last-update date. Use the feed's date for a reproducible capture.
day = dt.date.fromisoformat(latest["source_time"][:10])
with Path("oslo.dbf").open("r+b") as dbf:
    dbf.seek(1)
    dbf.write(bytes((day.year - 1900, day.month, day.day)))
Path("oslo.prj").write_text(
    'GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433]]\n'
)
Path("oslo.cpg").write_text("UTF-8\n")
attribution = "Station data: Oslo City Bike, NLOD 2.0. https://oslobysykkel.no/en/open-data/realtime\nTopographic map: © Kartverket, CC BY 4.0, zoom 11 grayscale tiles displayed at reduced opacity. https://www.kartverket.no/en/api-and-data/terms-of-use . Terrain Tiles: Mapzen. Norway terrain © Kartverket. Europe terrain produced using Copernicus data funded by the European Union, EU-DEM layers. https://registry.opendata.aws/terrain-tiles/ . Coastline: Natural Earth, public domain. https://www.naturalearthdata.com/\n"
Path("SOURCES.txt").write_text(attribution)
files = ["oslo.shp", "oslo.shx", "oslo.dbf", "oslo.prj", "oslo.cpg", "SOURCES.txt"]
with ZipFile("oslo-stations.zip", "w") as bundle:
    for name in files:
        entry = ZipInfo(name, date_time=(2026, 11, 22, 0, 0, 0))
        entry.compress_type = ZIP_DEFLATED
        bundle.writestr(entry, Path(name).read_bytes())

shore = json.loads((VENDOR / "shore.json").read_text())
payload = {
    "terrain": encoded((STATIC / "terrain.jpg").read_bytes()),
    "bounds": json.loads((STATIC / "terrain.json").read_text())["bounds"],
    "shp": encoded(Path("oslo.shp").read_bytes()),
    "dbf": encoded(Path("oslo.dbf").read_bytes()),
    "history": history,
    "shore": shore,
    "zip": encoded(Path("oslo-stations.zip").read_bytes()),
}
tiles = json.loads((VENDOR / "topo-z11/sources.json").read_bytes())
payload["tiles"] = [
    {
        "bounds": t["bounds"],
        "image": encoded((VENDOR / "topo-z11" / t["name"]).read_bytes()),
    }
    for t in tiles
]
template = (Path("/recipe/map.html")).read_text()
for token, path in [
    ("LEAFLET_CSS", "leaflet.css"),
    ("LEAFLET_JS", "leaflet.js"),
    ("SHAPEFILE_JS", "shapefile.js"),
]:
    template = template.replace("{{" + token + "}}", (STATIC / path).read_text())
template = template.replace(
    "{{DATA}}", encoded(json.dumps(payload, separators=(",", ":")).encode())
)
notices = "\n\n".join(
    p.read_text(encoding="latin-1" if p.name == "PyShp.txt" else "utf-8")
    for p in sorted(Path("/licenses").glob("*.txt"))
)
template = template.replace(
    "</html>",
    '<details><summary>Software licenses</summary><pre style="white-space:pre-wrap">'
    + escape(notices)
    + "</pre></details></html>",
)
Path("map.html").write_text(template)
receipt = {
    "station_features": len(ids),
    "latest_installed": len(set(ids) & latest["stations"].keys()),
    "rounds": len(history),
    "feed_time": latest["source_time"],
    "coastline_segments": len(shore),
    "attribution": attribution,
    "files": {
        n: hashlib.sha256(Path(n).read_bytes()).hexdigest()
        for n in files + ["oslo-stations.zip"]
    },
}
Path("product.json").write_text(json.dumps(receipt, indent=2) + "\n")
print(
    f"{len(ids)} point features; {len(history)} public feed editions; {len(shore)} coastal lines."
)
