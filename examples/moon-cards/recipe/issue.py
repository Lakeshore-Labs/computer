"""Issue two stable Anki editions from the printed measurements in the original EPUB."""

from datetime import datetime, timezone
import hashlib
import io
import json
from pathlib import Path
import sys
import xml.etree.ElementTree as ET
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED

sys.path.insert(0, "/vendor")
sys.path[:0] = [str(p) for p in sorted(Path("/vendor").glob("*.whl"))]
import genanki

book = Path("/book/four-moons.epub")
with ZipFile(book) as epub:
    measurements = ET.fromstring(epub.read("EPUB/measure.xhtml"))
    photo = epub.read("EPUB/galilean.jpg")
rows = [[cell.text for cell in tr] for tr in measurements.findall(".//{*}tbody/{*}tr")]
assert len(rows) == 4 and len({row[0] for row in rows}) == 4
image = Path("galilean-moons.jpg")
image.write_bytes(photo)
model = genanki.Model(
    1795384321,
    "Four moons / mean radius",
    fields=[{"name": "Moon"}, {"name": "Radius"}, {"name": "Image"}],
    templates=[
        {
            "name": "Mean radius",
            "qfmt": "<header>Jupiter / field notes</header><h1>{{Moon}}</h1><p>What is its mean radius?</p>",
            "afmt": '{{FrontSide}}<hr id="answer"><h2>{{Radius}}</h2>{{Image}}<footer>JPL satellite physical parameters. Composite image NASA/JPL/DLR.</footer>',
        }
    ],
    css=".card{font:21px sans-serif;text-align:left;background:#16272e;color:#f5f3e6;padding:6%}header,footer{font-size:.72em;color:#b6cfc8}h1{font:2.8em Georgia,serif}h2{color:#eac283}img{max-width:100%;height:auto}footer{margin-top:.6em}",
)


class RevisedNote(genanki.Note):
    """Give the corrected note its later modification time while preserving the package IDs."""

    def write_to_db(self, cursor, timestamp, deck_id, id_gen):
        super().write_to_db(cursor, timestamp + 3600, deck_id, id_gen)


editions = []
for edition in (1, 2):
    # Fixed edition times keep both package editions comparable during a later replay.
    dt = datetime(2026, 11, 21, 12 + edition, 0, tzinfo=timezone.utc)
    deck = genanki.Deck(1795384322, "Four moons of Jupiter")
    cards = []
    for name, radius, sigma in rows:
        answer = radius + " km"
        if edition == 2 and name == "Europa":
            answer += " (" + sigma + " km)"
        guid = genanki.guid_for("lakeshore/galilean/mean-radius", name)
        note_type = RevisedNote if edition == 2 and name == "Europa" else genanki.Note
        deck.add_note(
            note_type(
                model=model,
                guid=guid,
                fields=[
                    name,
                    answer,
                    '<img src="galilean-moons.jpg" alt="Composite of the four Galilean moons">',
                ],
            )
        )
        cards.append({"moon": name, "answer": answer, "guid": guid})
    pkg = genanki.Package(deck, media_files=[str(image)])
    raw = io.BytesIO()
    pkg.write_to_file(
        raw, timestamp=datetime(2026, 11, 21, 13, tzinfo=timezone.utc).timestamp()
    )
    raw.seek(0)
    dest = Path(f"edition-{edition}.apkg")
    with ZipFile(raw) as src, ZipFile(dest, "w") as out:
        for name in src.namelist():
            meta = ZipInfo(name, dt.timetuple()[:6])
            out.writestr(meta, src.read(name), compress_type=ZIP_DEFLATED)
    editions.append(
        {
            "file": dest.name,
            "sha256": hashlib.sha256(dest.read_bytes()).hexdigest(),
            "cards": cards,
        }
    )
image.unlink()
Path("issued.json").write_text(
    json.dumps(
        {
            "epub_sha256": hashlib.sha256(book.read_bytes()).hexdigest(),
            "embedded_jpeg_sha256": hashlib.sha256(photo).hexdigest(),
            "editions": editions,
        },
        indent=2,
        ensure_ascii=False,
    )
    + "\n"
)
print(
    "Issued two four-card editions from the EPUB. The second adds Europa's quoted measurement uncertainty."
)
