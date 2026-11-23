"""Draw USGS earthquake points over Natural Earth land using WASI Python."""
from datetime import datetime, timezone
from html import escape
import json
from pathlib import Path
from xml.etree import ElementTree

land = json.loads(Path("land.json").read_text())
feed = json.loads(Path("earthquakes.json").read_text())
events = [f for f in feed["features"] if f["properties"].get("type") == "earthquake"
          and isinstance(f["properties"].get("mag"), (float, int))
          and f["geometry"]["type"] == "Point"]
generated = datetime.fromtimestamp(feed["metadata"]["generated"] / 1000, timezone.utc)
left, top, width, height = 40, 143, 1000, 500

def project(lon, lat):
    return left + (lon + 180) * width / 360, top + (90 - lat) * height / 180

svg = ['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1080 727" role="img" aria-labelledby="title desc">',
       '<title id="title">A week of earthquakes, magnitude 4.5 and above</title>',
       f'<desc id="desc">{len(events)} earthquakes in the USGS past-week feed, generated {generated:%Y-%m-%d %H:%M} UTC.</desc>',
       '<rect width="1080" height="727" fill="#102b34"/>',
       '<g font-family="sans-serif" fill="#ebefdb">',
       '<text x="40" y="38" font-size="15" letter-spacing="2">EARTH OBSERVATORY   /   USGS WEEKLY FEED</text>',
       '<text x="38" y="91" font-size="42" font-family="serif">A week beneath our feet</text>',
       f'<text x="40" y="122" font-size="17">{len(events)} earthquakes of magnitude 4.5 or greater   ·   Feed generated {generated:%d %b %Y, %H:%M} UTC</text>',
       '</g><rect x="40" y="143" width="1000" height="500" fill="#163641"/>']
for deg in range(-150, 180, 30):
    x, _ = project(deg, 0)
    svg.append(f'<path d="M{x:.1f} {top} v{height}" stroke="#2f5055"/>')
for deg in range(-60, 90, 30):
    _, y = project(0, deg)
    svg.append(f'<path d="M{left} {y:.1f} h{width}" stroke="#2f5055"/>')
for feature in land["features"]:
    geo = feature["geometry"]
    polygons = [geo["coordinates"]] if geo["type"] == "Polygon" else geo["coordinates"]
    for polygon in polygons:
        rings = []
        for ring in polygon:
            points = ["%.1f,%.1f" % project(*point[:2]) for point in ring]
            rings.append("M" + "L".join(points) + "Z")
        svg.append(f'<path fill="#547271" fill-rule="evenodd" d="{"".join(rings)}"/>')
for event in sorted(events, key=lambda f: f["properties"]["mag"]):
    mag = event["properties"]["mag"]
    x, y = project(*event["geometry"]["coordinates"][:2])
    radius = 3 + 2.5 * (mag - 4.5)
    label = escape(f'M{mag:g} · {event["properties"].get("place") or event["id"]}')
    svg.append(f'<circle class="earthquake" cx="{x:.2f}" cy="{y:.2f}" r="{radius:.2f}" fill="#ffbd68" fill-opacity=".70" stroke="#ffe4ac"><title>{label}</title></circle>')
svg.append('<g font-family="sans-serif" fill="#d3dfd2" font-size="16"><text x="40" y="677">Larger circles indicate higher magnitude. Open the SVG file to inspect individual points.</text><text x="40" y="705">Observations: USGS Earthquake Hazards Program   ·   Land: Natural Earth   ·   Equirectangular projection</text></g></svg>')
Path("atlas.svg").write_text("\n".join(svg))
drawing = ElementTree.parse("atlas.svg")
circles = drawing.findall('.//{*}circle[@class="earthquake"]')
for circle, event in zip(circles, sorted(events, key=lambda f: f["properties"]["mag"]), strict=True):
    x, y = float(circle.get("cx")), float(circle.get("cy"))
    lon, lat = event["geometry"]["coordinates"][:2]
    assert left <= x <= left + width and top <= y <= top + height
    assert abs((x - left) * 360 / width - 180 - lon) < 0.01
    assert abs(90 - (y - top) * 180 / height - lat) < 0.01
print(f"{len(events)} earthquakes plotted from {len(feed['features'])} feed records.")
print(f"USGS feed generated {generated:%Y-%m-%d %H:%M:%S} UTC.")
