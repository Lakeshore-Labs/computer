"""Run a disposable Radicale server, Computer and an independent iCalendar reader."""

import argparse
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET

import icalendar

from agenda import verify as verify_agenda
from transfer import portable_event

HERE = Path(__file__).resolve().parent
# A day in a fictional rehearsal studio. Both players and the room must be free.
SEED = [
    ("room", "Stage setup", "0900", "0930"),
    ("flute", "Section coaching", "0915", "1015"),
    ("clarinet", "School ensemble", "1000", "1045"),
    ("room", "Piano move", "1045", "1100"),
    ("room", "Afternoon class", "1215", "1300"),
]
DAY = dt.date(2026, 11, 29)


def at(digits):
    return dt.datetime.combine(
        DAY, dt.time(int(digits[:2]), int(digits[2:]), tzinfo=dt.timezone.utc)
    )


def seed_file(name, title, begin, end):
    cal = icalendar.Calendar()
    cal.add("prodid", "-//Computer//Juniper Studio//EN")
    cal.add("version", "2.0")
    event = icalendar.Event()
    for key, value in [
        ("uid", name + "@juniper.example"),
        ("dtstamp", at("0800")),
        ("dtstart", at(begin)),
        ("dtend", at(end)),
        ("summary", title),
    ]:
        event.add(key, value)
    cal.add_component(event)
    return cal.to_ical()


def main(args):
    args.out.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix="juniper-dav-") as temp:
        work = Path(temp)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        url = f"http://127.0.0.1:{port}"
        password = secrets.token_urlsafe(20)
        users = work / "users"
        users.write_text("ensemble:" + password + "\n")
        users.chmod(0o600)
        conf = work / "radicale.ini"
        conf.write_text(
            f"[server]\nhosts = 127.0.0.1:{port}\n[auth]\ntype = htpasswd\nhtpasswd_filename = {users}\nhtpasswd_encryption = plain\n[storage]\nfilesystem_folder = {work / 'collections'}\n"
        )
        credentials = base64.b64encode(("ensemble:" + password).encode()).decode()
        local = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def request(method, path, body=None, **headers):
            req = urllib.request.Request(
                url + path,
                data=body,
                method=method,
                headers={"Authorization": "Basic " + credentials, **headers},
            )
            with local.open(req, timeout=8) as response:
                return response.read()

        log = (work / "activity.log").open("w")
        server = subprocess.Popen(
            [str(args.radicale), "--config", str(conf)], stdout=log, stderr=log
        )
        try:
            for attempt in range(60):
                if server.poll() is not None:
                    raise RuntimeError(
                        "The loopback calendar server stopped before startup."
                    )
                try:
                    request("GET", "/")
                    break
                except (OSError, urllib.error.URLError):
                    if attempt == 59:
                        raise
                    time.sleep(0.1)
            request("PROPFIND", "/ensemble/", Depth="0")
            for key in ("flute", "clarinet", "room"):
                body = (
                    f'<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:set><D:prop><D:displayname>{key}</D:displayname><C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set></D:prop></D:set></C:mkcalendar>'
                ).encode()
                request(
                    "MKCALENDAR",
                    f"/ensemble/{key}/",
                    body,
                    **{"Content-Type": "application/xml"},
                )
            for i, (key, title, a, b) in enumerate(SEED):
                request(
                    "PUT",
                    f"/ensemble/{key}/seed-{i}.ics",
                    seed_file("fixture-" + str(i), title, a, b),
                    **{"Content-Type": "text/calendar", "If-None-Match": "*"},
                )
            config = {
                "mounts": [
                    str(args.out.resolve()) + ":/work",
                    str(HERE) + ":/recipe:ro",
                ],
                "mcpServers": {
                    "calendar": {
                        "command": shutil.which("node"),
                        "args": [str(args.adapter.resolve())],
                        "env": {
                            "CALDAV_URL": url,
                            "CALDAV_USERNAME": "ensemble",
                            "CALDAV_PASSWORD": password,
                            "CALDAV_CALENDARS": "room,flute,clarinet",
                            "CALDAV_ALLOW_TOOLS": "get_free_busy,create_event",
                            "CALDAV_TIMEZONE": "UTC",
                        },
                    }
                },
            }
            settings = work / "computer.json"
            settings.write_text(json.dumps(config))
            settings.chmod(0o600)
            binary = [
                str(args.computer.resolve()),
                "-config",
                str(settings),
                "-python",
                str(args.python_dir.resolve() / "python.wasm"),
                "-python-lib",
                str(args.python_dir.resolve() / "lib"),
                "-workdir",
                "/work",
                "-c",
                "python3 /recipe/book.py",
            ]
            proc = subprocess.run(
                binary,
                capture_output=True,
                text=True,
                timeout=90,
                env={"HOME": temp, "PATH": os.defpath, "TZ": "UTC"},
            )
            if proc.returncode:
                raise RuntimeError(
                    (proc.stdout + proc.stderr).replace(password, "<temporary>")[-1000:]
                )
            result = json.loads((args.out / "booking.json").read_text())
            query = b'<C:calendar-query xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:D="DAV:"><D:prop><D:getetag/><C:calendar-data/></D:prop><C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"/></C:comp-filter></C:filter></C:calendar-query>'
            verified = []
            for key in ("room", "flute", "clarinet"):
                root = ET.fromstring(
                    request(
                        "REPORT",
                        f"/ensemble/{key}/",
                        query,
                        Depth="1",
                        **{"Content-Type": "application/xml"},
                    )
                )
                for response in root.findall("{DAV:}response"):
                    data = response.find(
                        ".//{urn:ietf:params:xml:ns:caldav}calendar-data"
                    )
                    if data is None:
                        continue
                    cal = icalendar.Calendar.from_ical(data.text)
                    event = cal.walk("VEVENT")[0]
                    record = {
                        "calendar": key,
                        "title": str(event["summary"]),
                        "start": event.decoded("dtstart").isoformat(),
                        "end": event.decoded("dtend").isoformat(),
                        "uid": str(event["uid"]),
                    }
                    verified.append(record)
                    if record["uid"] == result["uid"]:
                        path = response.findtext("{DAV:}href")
                        if not path or not path.startswith("/ensemble/room/"):
                            raise ValueError(
                                "The room event has an unexpected CalDAV URL."
                            )
                        (args.out / "room-get.ics").write_bytes(request("GET", path))
            matches = [v for v in verified if v["uid"] == result["uid"]]
            assert (
                len(matches) == 1
                and len(verified) == 6
                and matches[0]["calendar"] == "room"
            )
            assert matches[0]["start"] == result["start"] == at("1100").isoformat()
            assert matches[0]["end"] == result["end"] == at("1145").isoformat()
            assert matches[0]["title"] == result["subject"]
            originals = {
                (v["calendar"], v["title"], v["start"], v["end"])
                for v in verified
                if v["uid"] != result["uid"]
            }
            assert originals == {
                (k, t, at(a).isoformat(), at(b).isoformat()) for k, t, a, b in SEED
            }
            agenda = verify_agenda(work, url, password, args, result)
            synced = (args.out / "rehearsal.ics").read_bytes()
            (args.out / "rehearsal-portable.ics").write_bytes(portable_event(synced))
            wire = {}
            for filename in ("room-get.ics", "rehearsal.ics", "rehearsal-portable.ics"):
                raw = (args.out / filename).read_bytes()
                wire[filename] = {
                    "sha256": hashlib.sha256(raw).hexdigest(),
                    "LF": raw.count(b"\n"),
                    "CRLF": raw.count(b"\r\n"),
                    "TZID_UTC": raw.count(b";TZID=UTC:"),
                }
            (args.out / "calendar-check.json").write_text(
                json.dumps(
                    {
                        "verified_events": verified,
                        "native_agenda": agenda,
                        "transfer": wire,
                        "file_sha256": hashlib.sha256(
                            (args.out / "rehearsal.ics").read_bytes()
                        ).hexdigest(),
                        "reader": "CalDAV REPORT and native Python icalendar",
                    },
                    indent=2,
                )
                + "\n"
            )
            print(
                result["start"],
                result["end"],
                len(verified),
                "independently parsed events.",
            )
        finally:
            server.terminate()
            try:
                server.wait(timeout=6)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()
            log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in (
        "computer",
        "python-dir",
        "radicale",
        "adapter",
        "vdirsyncer",
        "khal",
        "out",
    ):
        parser.add_argument("--" + name, type=Path, required=True)
    main(parser.parse_args())
