"""Read the two festival publications from a fresh read-only Computer and parse both calendars."""
import asyncio
import hashlib
import json
import sys
from pathlib import Path

from icalendar import Calendar
from mcp import ClientSession, StdioServerParameters, stdio_client

FILMS = ("After the Reeds", "The Quiet Platform", "Small Orbits")
EXPECTED = {
    "original": (("18:00", "18:25"), ("18:30", "19:02"), ("19:07", "19:25")),
    "amended": (("18:00", "18:40"), ("18:45", "19:17"), ("19:22", "19:40")),
}


async def main(binary, directory):
    work = Path(directory).resolve()
    params = StdioServerParameters(command=binary, args=["-mount", str(work) + ":/work:ro", "mcp"])
    results = {}
    uids = {}
    async with stdio_client(params) as (reader, writer):
        async with ClientSession(reader, writer) as client:
            init = await client.initialize()
            for edition, sequence in (("original", 0), ("amended", 1)):
                name = edition + ".ics"
                reply = await client.call_tool("read", {"path": name})
                if reply.is_error or len(reply.content) != 1 or reply.content[0].type != "text":
                    raise RuntimeError(f"The Python client could not read {name}.")
                content = reply.content[0].text.encode()
                if content != (work / name).read_bytes() or b"\r\n" not in content:
                    raise ValueError("The client bytes differ from the mounted calendar.")
                events = Calendar.from_ical(content).walk("VEVENT")
                for event in events:
                    for field in ("DTSTART", "DTEND"):
                        stamp = event.decoded(field)
                        if stamp.date().isoformat() != "2026-11-23" or stamp.utcoffset().total_seconds() != 0:
                            raise ValueError("The calendar’s screening day or UTC offset changed.")
                schedule = [(str(event["SUMMARY"]), event.decoded("DTSTART").strftime("%H:%M"),
                             event.decoded("DTEND").strftime("%H:%M")) for event in events]
                expected = [(title, *times) for title, times in zip(FILMS, EXPECTED[edition])]
                if schedule != expected or any(int(e["SEQUENCE"]) != sequence for e in events):
                    raise ValueError("The calendar has an unexpected schedule or revision.")
                uids[edition] = [str(e["UID"]) for e in events]
                results[edition] = {"read_bytes": len(content), "sha256": hashlib.sha256(content).hexdigest(),
                                    "schedule_utc": schedule, "sequence": sequence}
    if len(set(uids["original"])) != 3 or uids["original"] != uids["amended"]:
        raise ValueError("The revised events must preserve their three distinct identities.")
    print(json.dumps({"client": "Python SDK", "server": init.server_info.name,
                      "read_only_mount": True, "stable_event_ids": uids["original"],
                      "calendars": results}, indent=2))


if __name__ == "__main__":
    asyncio.run(main(*sys.argv[1:]))
