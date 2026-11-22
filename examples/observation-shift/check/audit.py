#!/usr/bin/env python3
"""Independent audit of a published field-shift desk against its source observations.

usage: python3 -I audit.py --input DIR_WITH_observations.json --output DIR_WITH_published_files [--report FILE]
Stdlib only. Does not import or run publish.py; every expectation is recomputed from observations.json.
Exit 0 = all checks pass, 1 = failures.
"""

import argparse, csv, datetime as dt, hashlib, json, re, sqlite3, sys
from pathlib import Path

ap = argparse.ArgumentParser()
ap.add_argument("--input", required=True, type=Path)
ap.add_argument("--output", required=True, type=Path)
ap.add_argument("--report", type=Path)
a = ap.parse_args()

results, notes = [], []


def check(name, ok, detail=""):
    results.append({"check": name, "ok": bool(ok), "detail": str(detail)})


def parse(ts):
    return dt.datetime.fromisoformat(ts)


src_path = a.input / "observations.json"
raw = src_path.read_bytes()
src = json.loads(raw)
SHA = re.compile(r"^[0-9a-f]{64}$")

# ---- source sanity ----
check(
    "source: top-level keys",
    set(src)
    >= {"tides", "bikes", "hosted", "desk", "single_session", "collector_complete"},
    sorted(src),
)
tides, bikes, hosted, desk = src["tides"], src["bikes"], src["hosted"], src["desk"]
for name, seq, key in (
    ("tides", tides, "at"),
    ("bikes", bikes, "at"),
    ("hosted", hosted, "at"),
    ("desk", desk, "checked_utc"),
):
    ts = [x[key] for x in seq]
    check(
        f"source: {name} timestamps parse, strictly increasing, UTC",
        all(parse(t).utcoffset() == dt.timedelta(0) for t in ts)
        and ts == sorted(set(ts)),
        f"{len(ts)} entries",
    )
check(
    "source: every tide batch has 3 distinct stations, numeric metres, observation <= capture",
    all(
        len(b["readings"]) == 3
        and len({r["station"] for r in b["readings"]}) == 3
        and all(
            isinstance(r["meters"], (int, float))
            and parse(r["observation_utc"].replace(" ", "T") + "+00:00")
            <= parse(b["at"])
            for r in b["readings"]
        )
        for b in tides
    ),
)
check(
    "source: tide station set constant",
    len({tuple(sorted(r["station"] for r in b["readings"])) for b in tides}) == 1,
)
check(
    "source: bike feeds consistent (3 stops, stations_with_bikes<=station_count, stop bikes<=total, sha256)",
    all(
        len(b["stations"]) == 3
        and b["stations_with_bikes"] <= b["station_count"]
        and sum(s["bikes"] for s in b["stations"].values()) <= b["bikes_available"]
        and SHA.match(b["feed_sha256"])
        and parse(b["feed_at"]) <= parse(b["at"]) + dt.timedelta(minutes=5)
        for b in bikes
    ),
)
check(
    "source: hosted rounds have astro/mslearn/svelte, sha256, passed is bool",
    all(
        [c["name"] for c in h["checks"]] == ["astro", "mslearn", "svelte"]
        and all(
            isinstance(c["passed"], bool) and SHA.match(c["sha256"])
            for c in h["checks"]
        )
        for h in hosted
    ),
)
ages = [d["session_age_seconds"] for d in desk]
check(
    "source: client session age non-decreasing and single_session==1",
    ages == sorted(ages) and src["single_session"] == 1,
    ages,
)
check(
    "source: client reads are read-only, independent, sha256",
    all(
        d["source_read_only"]
        and d["independent_read_only_session"]
        and SHA.match(d["guest_sha256"])
        for d in desk
    ),
)
check(
    "source: client age agrees with elapsed time (+-60s)",
    all(
        abs(
            (parse(d["checked_utc"]) - parse(desk[0]["checked_utc"])).total_seconds()
            + ages[0]
            - d["session_age_seconds"]
        )
        <= 60
        for d in desk
    ),
    [(d["checked_utc"][11:19], d["session_age_seconds"]) for d in desk],
)
if not src["collector_complete"]:
    notes.append(
        "source collector_complete is false: sample was still being captured; refresh before final freeze"
    )

# ---- expected values ----
exp = {"tide": [], "bike": [], "stop": [], "hosted": [], "client": []}
for b in tides:
    for r in b["readings"]:
        exp["tide"].append(
            (
                b["at"],
                r["station"],
                r["city"],
                r["observation_utc"],
                r["meters"],
                r["quality"],
            )
        )
for b in bikes:
    exp["bike"].append(
        (
            b["at"],
            b["feed_at"],
            b["station_count"],
            b["stations_with_bikes"],
            b["bikes_available"],
            b["feed_sha256"],
        )
    )
    for k, v in b["stations"].items():
        exp["stop"].append((b["at"], k, v["name"], v["bikes"], int(v["renting"])))
for h in hosted:
    for c in h["checks"]:
        exp["hosted"].append(
            (
                h["at"],
                c["name"],
                int(c["passed"]),
                c["seconds"],
                c["response_bytes"],
                c["sha256"],
            )
        )
for d in desk:
    exp["client"].append(
        (
            d["checked_utc"],
            d["batch"],
            d["session_age_seconds"],
            int(d["independent_read_only_session"]),
            d["guest_sha256"],
        )
    )
times = sorted(
    {b["at"] for b in tides}
    | {b["at"] for b in bikes}
    | {h["at"] for h in hosted}
    | {d["checked_utc"] for d in desk}
)
exp_summary = {
    "times": len(times),
    "tide_rows": len(exp["tide"]),
    "tide_batches": len(tides),
    "bike_feeds": len(bikes),
    "hosted_operations": len(exp["hosted"]),
    "hosted_passed": sum(x[2] for x in exp["hosted"]),
    "persistent_client_reads": len(desk),
    "oldest": times[0],
    "newest": times[-1],
    "one_persistent_session": src["single_session"] == 1,
    "capture_ended": src["collector_complete"],
    "source_sha256": hashlib.sha256(raw).hexdigest(),
}

# ---- summary.json ----
summ = json.loads((a.output / "summary.json").read_text())
check(
    "summary.json equals recomputed summary",
    summ == exp_summary,
    {k: (summ.get(k), v) for k, v in exp_summary.items() if summ.get(k) != v}
    or "identical",
)
check(
    "summary: hosted_passed <= hosted_operations",
    summ["hosted_passed"] <= summ["hosted_operations"],
)

# ---- sqlite ----
db = sqlite3.connect(f"file:{a.output / 'shift.sqlite'}?mode=ro", uri=True)
check(
    "sqlite: integrity_check ok",
    db.execute("pragma integrity_check").fetchone()[0] == "ok",
)
check(
    "sqlite: tables exactly tide,bike,stop,hosted,client",
    {r[0] for r in db.execute("select name from sqlite_master where type='table'")}
    == set(exp),
)
for t, rows in exp.items():
    got = sorted(db.execute(f"select * from {t}").fetchall(), key=repr)
    want = sorted(rows, key=repr)
    check(
        f"sqlite: {t} rows match source exactly ({len(want)})",
        got == want,
        "" if got == want else f"got {len(got)} want {len(want)}",
    )
check(
    "sqlite: bike row count equals summary bike_feeds",
    db.execute("select count(*) from bike").fetchone()[0] == summ["bike_feeds"],
)
check(
    "sqlite: hosted passed count == summary",
    db.execute("select coalesce(sum(passed),0) from hosted").fetchone()[0]
    == summ["hosted_passed"],
)
check(
    "sqlite: every stop/hosted/client capture has parent where applicable",
    db.execute(
        "select count(*) from stop where capture not in (select capture from bike)"
    ).fetchone()[0]
    == 0,
)
db.close()

# ---- timeline.csv ----
with (a.output / "timeline.csv").open(newline="") as f:
    rd = list(csv.reader(f))
check(
    "timeline.csv header",
    rd[0] == ["capture_utc", "source", "measurement_count"],
    rd[0],
)
want = []
for label, seq, key, n in (
    ("NOAA", tides, "at", lambda x: len(x["readings"])),
    ("Oslo GBFS", bikes, "at", lambda x: 1),
    ("Hosted MCP", hosted, "at", lambda x: len(x["checks"])),
    ("Persistent Computer client", desk, "checked_utc", lambda x: 1),
):
    want += [(x[key], label, str(n(x))) for x in seq]
got = [tuple(r) for r in rd[1:]]
check(
    "timeline.csv rows equal recomputed set",
    sorted(got) == sorted(want),
    f"{len(got)} vs {len(want)}",
)
check(
    "timeline.csv chronologically ordered",
    [g[0] for g in got] == sorted(g[0] for g in got),
)
check(
    "timeline.csv distinct capture times == summary.times",
    len({g[0] for g in got}) == summ["times"],
)

# ---- shift.html ----
html = (a.output / "shift.html").read_text()
check("shift.html: placeholder substituted", "__SHIFT__" not in html)
m = re.search(
    r'<script type="application/json" id="observations">(.*?)</script>', html, re.S
)
check(
    "shift.html: embedded payload present, no raw '<'",
    bool(m) and "<" not in m.group(1),
)
if m:
    p = json.loads(m.group(1))
    check("shift.html payload times == recomputed", p["times"] == times)
    check("shift.html payload summary == summary.json", p["summary"] == summ)
    for t, rows in exp.items():
        got = sorted((tuple(r.values()) for r in p["series"][t]), key=repr)
        check(
            f"shift.html payload {t} rows match source", got == sorted(rows, key=repr)
        )
ext = re.findall(r'(?:src|href)=["\'](https?:[^"\']+)', html)
check("shift.html: no external script/style/src/href", not ext, ext)
check(
    "shift.html: no remote fetch/import code",
    not re.search(
        r"fetch\(|XMLHttpRequest|import\(|@import|url\(\s*['\"]?https?:", html
    ),
)

failed = [r for r in results if not r["ok"]]
rep = {
    "input": "observations.json",
    "output": ["shift.html", "shift.sqlite", "summary.json", "timeline.csv"],
    "source_sha256": exp_summary["source_sha256"],
    "checks": len(results),
    "failed": len(failed),
    "notes": notes,
    "results": results,
}
if a.report:
    a.report.write_text(json.dumps(rep, indent=2) + "\n")
for r in results:
    print(
        ("PASS " if r["ok"] else "FAIL ")
        + r["check"]
        + ("" if r["ok"] else f"  -> {r['detail']}")
    )
for n in notes:
    print("NOTE " + n)
print(f"{len(results) - len(failed)}/{len(results)} checks passed")
sys.exit(1 if failed else 0)
