"""Save an EPUB and a calendar as a tagged OCI image-layout artifact."""

from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path

LAYOUT = Path("layout")
BLOBS = LAYOUT / "blobs/sha256"
BLOBS.mkdir(parents=True, exist_ok=True)


def serialize(value):
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode()


def save(data, kind, filename=None):
    digest = hashlib.sha256(data).hexdigest()
    target = BLOBS / digest
    if target.exists():
        if target.read_bytes() != data:
            raise ValueError("stored blob did not match its digest")
    else:
        tmp = target.with_suffix(".tmp")
        tmp.write_bytes(data)
        os.replace(tmp, target)
    descriptor = {"mediaType": kind, "digest": "sha256:" + digest, "size": len(data)}
    if filename:
        descriptor["annotations"] = {"org.opencontainers.image.title": filename}
    return descriptor


event = json.loads(Path("session.json").read_text())
tag = event["edition"]
start = datetime.fromisoformat(event["start"]).astimezone(timezone.utc)
end = start + timedelta(minutes=45)
icaldate = lambda t: t.strftime("%Y%m%dT%H%M%SZ")
lines = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Computer//Reading Room//EN",
    "BEGIN:VEVENT",
    "UID:four-moons@library.example",
    "DTSTAMP:20261122T200000Z",
    "SEQUENCE:" + str(event["sequence"]),
    "DTSTART:" + icaldate(start),
    "DTEND:" + icaldate(end),
    "SUMMARY:Four moons reading room",
    "LOCATION:West reading room",
    "END:VEVENT",
    "END:VCALENDAR",
]
cal = ("\r\n".join(lines) + "\r\n").encode()
book = Path("/book/four-moons.epub").read_bytes()
config = save(b"{}", "application/vnd.oci.empty.v1+json")
layers = [
    save(book, "application/epub+zip", "four-moons.epub"),
    save(cal, "text/calendar", "reading-room.ics"),
]
manifest = {
    "schemaVersion": 2,
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "artifactType": "application/epub+zip",
    "config": config,
    "layers": layers,
}
desc = save(serialize(manifest), manifest["mediaType"])
desc["annotations"] = {"org.opencontainers.image.ref.name": tag}
index = LAYOUT / "index.json"
refs = json.loads(index.read_bytes())["manifests"] if index.exists() else []
refs = [d for d in refs if d["annotations"]["org.opencontainers.image.ref.name"] != tag]
refs.append(desc)
refs.sort(key=lambda d: d["annotations"]["org.opencontainers.image.ref.name"])
(LAYOUT / "oci-layout").write_bytes(serialize({"imageLayoutVersion": "1.0.0"}))
tmp = LAYOUT / "index.tmp"
tmp.write_bytes(
    serialize(
        {
            "schemaVersion": 2,
            "mediaType": "application/vnd.oci.image.index.v1+json",
            "manifests": refs,
        }
    )
)
os.replace(tmp, index)
print(
    json.dumps(
        {
            "edition": tag,
            "manifest": desc["digest"],
            "book": layers[0]["digest"],
            "calendar": layers[1]["digest"],
            "start": event["start"],
        }
    )
)
