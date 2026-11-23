"""Inside Computer, join four observation histories and issue the offline shift desk."""

import csv
import hashlib
import json
from pathlib import Path
import sqlite3
import sys

root, out = map(Path, sys.argv[1:3])
data = json.loads((root / "observations.json").read_text())
db = sqlite3.connect(out / "shift.sqlite")
with db:
    db.executescript("""
create table tide (capture text, station text, city text, observation text, metres real, quality text, primary key(capture, station));
create table bike (capture text primary key, feed_time text, stations integer, stations_with_bikes integer, bikes integer, digest text);
create table stop (capture text, station text, name text, bikes integer, renting integer, primary key(capture,station));
create table hosted (capture text, name text, passed integer, seconds real, bytes integer, digest text, primary key(capture,name));
create table client (capture text primary key, batch text, age_seconds integer, independent_reader integer, digest text);
""")
    for batch in data["tides"]:
        for r in batch["readings"]:
            db.execute(
                "insert into tide values (?,?,?,?,?,?)",
                (
                    batch["at"],
                    r["station"],
                    r["city"],
                    r["observation_utc"],
                    r["meters"],
                    r["quality"],
                ),
            )
    for b in data["bikes"]:
        db.execute(
            "insert into bike values (?,?,?,?,?,?)",
            (
                b["at"],
                b["feed_at"],
                b["station_count"],
                b["stations_with_bikes"],
                b["bikes_available"],
                b["feed_sha256"],
            ),
        )
        db.executemany(
            "insert into stop values (?,?,?,?,?)",
            [
                (b["at"], key, v["name"], v["bikes"], v["renting"])
                for key, v in b["stations"].items()
            ],
        )
    for b in data["hosted"]:
        for r in b["checks"]:
            db.execute(
                "insert into hosted values (?,?,?,?,?,?)",
                (
                    b["at"],
                    r["name"],
                    r["passed"],
                    r["seconds"],
                    r["response_bytes"],
                    r["sha256"],
                ),
            )
    for r in data["desk"]:
        db.execute(
            "insert into client values (?,?,?,?,?)",
            (
                r["checked_utc"],
                r["batch"],
                r["session_age_seconds"],
                r["independent_read_only_session"],
                r["guest_sha256"],
            ),
        )
db.row_factory = sqlite3.Row


def rows(sql):
    return [dict(v) for v in db.execute(sql)]


times = [
    r["capture"]
    for r in rows(
        "select capture from tide union select capture from bike union select capture from hosted union select capture from client order by capture"
    )
]
series = {
    table: rows(f"select * from {table} order by capture")
    for table in ("tide", "bike", "stop", "hosted", "client")
}
summary = {
    "times": len(times),
    "tide_rows": len(series["tide"]),
    "tide_batches": len(data["tides"]),
    "bike_feeds": len(series["bike"]),
    "hosted_operations": len(series["hosted"]),
    "hosted_passed": sum(x["passed"] for x in series["hosted"]),
    "persistent_client_reads": len(series["client"]),
    "oldest": times[0],
    "newest": times[-1],
    "one_persistent_session": data["single_session"] == 1,
    "capture_ended": data["collector_complete"],
    "source_sha256": hashlib.sha256(
        (root / "observations.json").read_bytes()
    ).hexdigest(),
}
(out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
with (out / "timeline.csv").open("w", newline="") as f:
    log = csv.writer(f, lineterminator="\n")
    log.writerow(("capture_utc", "source", "measurement_count"))
    for t in times:
        for table, label in [
            ("tide", "NOAA"),
            ("bike", "Oslo GBFS"),
            ("hosted", "Hosted MCP"),
            ("client", "Persistent Computer client"),
        ]:
            n = db.execute(
                f"select count(*) from {table} where capture=?", (t,)
            ).fetchone()[0]
            if n:
                log.writerow((t, label, n))
db.close()
payload = json.dumps(
    {"times": times, "series": series, "summary": summary}, ensure_ascii=False
).replace("<", "\\u003c")
(out / "shift.html").write_text(
    Path("/recipe/reader.html").read_text().replace("__SHIFT__", payload)
)
print(json.dumps(summary))
