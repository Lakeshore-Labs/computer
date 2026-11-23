"""Check the printed labels, source hashes and every image embedded by the guest."""
import base64
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import sys


class Study(HTMLParser):
    def __init__(self):
        super().__init__()
        self.images = {}
        self.text = []

    def handle_starttag(self, name, attrs):
        if name == "img":
            tag = dict(attrs)
            prefix = "data:image/jpeg;base64,"
            if not tag["src"].startswith(prefix):
                raise ValueError("An image requires an external source.")
            self.images[tag["alt"]] = base64.b64decode(tag["src"][len(prefix):], validate=True)

    def handle_data(self, data):
        self.text.append(data.strip())


def check(source, output):
    page = Study()
    page.feed((output / "study.html").read_text())
    cleveland = json.loads((source / "cleveland/record.json").read_text())["data"]
    getty = json.loads((source / "getty/record.json").read_text())
    assert cleveland["share_license_status"] == "CC0" and getty["rights"].endswith("/publicdomain/zero/1.0/")
    assert cleveland["creation_date"] == json.loads((source / "getty/year.json").read_text()) == "1889"
    museums = {"cleveland": (cleveland["title"], cleveland["creditline"], "1947.209"),
              "getty": (getty["label"]["en"][0], "The J. Paul Getty Museum, 90.PA.20", "90.PA.20")}
    assert cleveland["accession_number"] == museums["cleveland"][2]
    assert next(v["value"]["en"][0] for v in getty["metadata"] if v["label"]["en"] == ["Accession Number"]) == museums["getty"][2]
    assert set((output / "museums.txt").read_text().splitlines()) == set(museums)
    assert len(page.images) == 2
    receipts = []
    for museum, (title, credit, accession) in museums.items():
        original = (source / museum / "painting.jpg").read_bytes()
        thumbnail = (output / (museum + ".jpg")).read_bytes()
        assert original.startswith(b"\xff\xd8\xff") and thumbnail.startswith(b"\xff\xd8\xff")
        assert page.images[title] == thumbnail and title in page.text and credit in page.text
        digest = hashlib.sha256(original).hexdigest()
        assert digest + "  /collection/" + museum + "/painting.jpg" in (output / "guest.log").read_text()
        receipts.append({"museum": museum, "title": title, "accession": accession, "source_bytes": len(original),
                         "source_sha256": digest, "embedded_bytes": len(thumbnail),
                         "embedded_sha256": hashlib.sha256(thumbnail).hexdigest()})
    return {"mount": "in-process rclone combine backend and vfs.VFS through afero.FromIOFS",
            "mount_mode": "read only", "images": receipts, "html_bytes": (output / "study.html").stat().st_size}


if __name__ == "__main__":
    print(json.dumps(check(Path(sys.argv[1]), Path(sys.argv[2])), indent=2))
