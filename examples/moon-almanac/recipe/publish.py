"""Publish a reflowable EPUB from two captured data sources and a credited NASA image."""

import csv
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED, ZIP_STORED

SOURCE = Path("/source")
PHOTO = "https://science.nasa.gov/photojournal/the-galilean-satellites-3/"
JPL = "https://ssd.jpl.nasa.gov/sats/phys_par/"
TITLE = "Four moons of Jupiter"


class TableRows(HTMLParser):
    """JPL's table uses optional closing td tags."""

    def __init__(self):
        super().__init__()
        self.rows = []
        self.row = None
        self.cell = None

    def finish_cell(self):
        if self.cell is not None:
            self.row.append(" ".join("".join(self.cell).split()))
            self.cell = None

    def handle_starttag(self, tag, attrs):
        if tag == "tr":
            self.row = []
        elif tag in ("td", "th") and self.row is not None:
            self.finish_cell()
            self.cell = []

    def handle_endtag(self, tag):
        if tag in ("td", "th"):
            self.finish_cell()
        elif tag == "tr" and self.row is not None:
            self.finish_cell()
            self.rows.append(self.row)
            self.row = None

    def handle_data(self, data):
        if self.cell is not None:
            self.cell.append(data)


def xhtml(title, body):
    return f"""<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="en" lang="en">
<head><title>{title}</title><link rel="stylesheet" href="book.css" type="text/css"/></head>
<body>{body}</body></html>"""


wd = list(
    csv.DictReader((SOURCE / "wikidata.csv").read_text().splitlines(), delimiter=";")
)
ids = {r["name"]: r["moon"] for r in wd if r["parent"] == "Q319"}
if len(ids) != 4:
    raise ValueError("expected four distinct Jupiter satellites from Wikidata")
table = TableRows()
table.feed((SOURCE / "jpl.html").read_text())
rows = [
    {
        "name": r[1],
        "id": ids[r[1]],
        "code": r[2],
        "radius_km": r[6],
        "sigma_km": r[7],
        "density": r[9],
    }
    for r in table.rows
    if len(r) == 12 and r[0] == "Jupiter" and r[1] in ids
]
if {r["name"] for r in rows} != set(ids) or len(rows) != 4:
    raise ValueError("JPL and Wikidata did not yield four matching satellites")
rows.sort(key=lambda row: int(row["code"]))
Path("moons.json").write_text(json.dumps(rows, indent=2) + "\n")
photo = (SOURCE / "galilean.jpg").read_bytes()
cover = xhtml(
    TITLE,
    """<main><p class="overline">A POCKET ASTRONOMY GUIDE</p><h1>Four moons<br/>of Jupiter</h1>
<figure><img src="galilean.jpg" alt="From left to right, the disks of Ganymede, Callisto, Io and Europa."/>
<figcaption>Ganymede, Callisto, Io and Europa. NASA/JPL/DLR, PIA00601.</figcaption></figure>
<p>The four disks share one image scale. Which moon looks largest? Turn the page to compare their measured radii.</p></main>""",
)
data_rows = "".join(
    f'<tr><th scope="row">{r["name"]}</th><td>{float(r["radius_km"]):,.2f}</td><td>±{r["sigma_km"]}</td></tr>'
    for r in rows
)
measure = xhtml(
    "Measure the moons",
    f"""<main><p class="overline">PHYSICAL MEASUREMENTS</p><h1>Measure the moons</h1>
<p>Mean radius is the radius of a sphere with the same volume as the moon. The figures below come from JPL’s planetary satellite table.</p>
<table><caption>Mean radii and their quoted uncertainties, in kilometers</caption><thead><tr><th>Moon</th><th>Radius</th><th>± km</th></tr></thead><tbody>{data_rows}</tbody></table>
<p>Ganymede is largest by this measure. Europa is the smallest of these four.</p></main>""",
)
refs = "".join(
    f'<li><a href="https://www.wikidata.org/wiki/{r["id"]}">{r["name"]} · {r["id"]}</a></li>'
    for r in rows
)
credits = xhtml(
    "Sources and credits",
    f'''<main><p class="overline">OBSERVING NOTES</p><h1>Sources and credits</h1><p>The original composite combines Voyager and Galileo observations. Image credit NASA/JPL/DLR.</p>
<ul><li><a href="{PHOTO}">NASA image PIA00601</a></li><li><a href="{JPL}">JPL satellite physical parameters</a></li>{refs}</ul>
<p>The publisher joins Wikidata’s satellite identifiers to JPL’s names. The four linked records list Jupiter as their parent body.</p></main>''',
)
chapters = [
    ("cover", "Four moons of Jupiter", cover),
    ("measure", "Measure the moons", measure),
    ("credits", "Sources and credits", credits),
]
nav = xhtml(
    "Contents",
    '<nav epub:type="toc" id="toc"><h1>Contents</h1><ol>'
    + "".join(f'<li><a href="{id}.xhtml">{title}</a></li>' for id, title, _ in chapters)
    + "</ol></nav>",
)
css = """body{font:1.1em/1.55 Georgia,serif;color:#172330;background:#f8f5ee;margin:0;padding:1.4em}main{max-width:38em;margin:auto}
h1{font-weight:normal;line-height:.98;font-size:2.6em;margin:.45em 0}.overline,figcaption,table,caption{font-family:sans-serif}.overline{font-size:.69em;letter-spacing:.14em;font-weight:bold;color:#6b462f}
figure{margin:1.2em 0 .6em}img{width:100%;height:auto}figcaption,caption{font-size:.76em;color:#495660}table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}
caption{text-align:left;margin:.8em 0}td,th{text-align:right;padding:.45em .2em;border-bottom:1px solid #b5aea0}th:first-child{text-align:left}a{color:#165e6b}
"""
manifest = "".join(
    f'<item id="{id}" href="{id}.xhtml" media-type="application/xhtml+xml"/>'
    for id, _, _ in chapters
)
spine = "".join(f'<itemref idref="{id}"/>' for id, _, _ in chapters)
package = f"""<?xml version="1.0" encoding="UTF-8"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="book-id">urn:example:computer:four-moons:2026-11-22</dc:identifier>
<dc:title>{TITLE}</dc:title><dc:language>en</dc:language><meta property="dcterms:modified">2026-11-22T00:00:00Z</meta></metadata>
<manifest>{manifest}<item id="nav" href="nav.xhtml" properties="nav" media-type="application/xhtml+xml"/>
<item id="photo" href="galilean.jpg" media-type="image/jpeg"/><item id="style" href="book.css" media-type="text/css"/></manifest><spine>{spine}</spine></package>"""
container = """<?xml version="1.0" encoding="UTF-8"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="EPUB/package.opf" media-type="application/oebps-package+xml"/></rootfiles></container>"""
assets = [
    ("mimetype", "application/epub+zip"),
    ("META-INF/container.xml", container),
    ("EPUB/package.opf", package),
    ("EPUB/nav.xhtml", nav),
    ("EPUB/book.css", css),
    ("EPUB/galilean.jpg", photo),
]
assets.extend((f"EPUB/{id}.xhtml", doc) for id, _, doc in chapters)
with ZipFile("four-moons.epub", "w") as archive:
    for name, data in assets:
        info = ZipInfo(name, date_time=(2026, 11, 22, 0, 0, 0))
        info.compress_type = ZIP_STORED if name == "mimetype" else ZIP_DEFLATED
        archive.writestr(info, data)
print(
    f"{len(chapters)} chapters, {len(rows)} moons; source image SHA-256 {hashlib.sha256(photo).hexdigest()}"
)
