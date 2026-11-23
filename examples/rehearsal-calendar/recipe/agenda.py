"""Sync the room into a fresh vdir, then read the booked rehearsal using khal."""

import os
import subprocess

import icalendar


def verify(work, url, password, args, booking):
    vdir = work / "room"
    vdir.mkdir()
    temp_env = {
        "HOME": str(work),
        "PATH": os.defpath,
        "TZ": "UTC",
        "LANG": "en_US.UTF-8",
        "XDG_DATA_HOME": str(work / "data"),
        "NO_COLOR": "1",
    }
    config = work / "vdirsyncer.ini"
    config.write_text(f'''[general]
status_path = "{work / "sync-status"}"
[pair studio]
a = "remote"
b = "local"
collections = null
[storage remote]
type = "caldav"
url = "{url}/ensemble/room/"
username = "ensemble"
password = "{password}"
read_only = true
[storage local]
type = "filesystem"
path = "{vdir}"
fileext = ".ics"
''')
    config.chmod(0o600)
    sync = [str(args.vdirsyncer.resolve()), "-c", str(config)]
    for command in ("discover", "sync"):
        result = subprocess.run(
            [*sync, command, "studio"],
            env=temp_env,
            capture_output=True,
            text=True,
            timeout=20,
        )
        if result.returncode:
            raise RuntimeError(result.stderr.replace(password, "<temporary>")[-1200:])
    config = work / "khal.ini"
    config.write_text(f"""[calendars]
[[room]]
path = {vdir}
color = dark green
[locale]
dateformat = %Y-%m-%d
longdateformat = %Y-%m-%d
timeformat = %H:%M
default_timezone = UTC
local_timezone = UTC
""")
    command = [
        str(args.khal.resolve()),
        "-c",
        str(config),
        "list",
        "--day-format",
        "",
        "--format",
        "{start-time}–{end-time} {title}",
        "2026-11-29",
        "2026-11-30",
    ]
    agenda = subprocess.run(
        command, env=temp_env, check=True, capture_output=True, text=True, timeout=20
    ).stdout.strip()
    lines = [line for line in agenda.splitlines() if line.strip()]
    if len(lines) != 4 or "11:00–11:45 Last Boat Home rehearsal" not in lines:
        raise ValueError(
            "The native agenda differs from the room schedule: " + repr(lines)
        )
    (args.out / "agenda.txt").write_text(agenda + "\n")
    events = []
    for file in sorted(vdir.glob("*.ics")):
        event = icalendar.Calendar.from_ical(file.read_bytes()).walk("VEVENT")[0]
        events.append(str(event["uid"]))
        if events[-1] == booking["uid"]:
            (args.out / "rehearsal.ics").write_bytes(file.read_bytes())
    if len(events) != 4 or events.count(booking["uid"]) != 1:
        raise ValueError("The synced vdir does not contain the four expected events.")
    return {
        "reader": "vdirsyncer and khal",
        "synced_ics_files": len(events),
        "agenda": lines,
    }
