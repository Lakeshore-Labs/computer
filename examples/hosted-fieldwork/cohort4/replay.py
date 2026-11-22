"""Run the fourth hosted cohort through Computer and retain succinct, reproducible checks."""

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
from zoneinfo import ZoneInfo

HERE = Path(__file__).resolve().parent
GHANA_NATIONAL = 30_832_019
ACCRA = 5_455_692


def check(case, d, text):
    if case == "musokit-eb-dorian":
        return {
            "seven scale degrees": d["notes"]
            == ["Eb", "F", "Gb", "Ab", "Bb", "C", "Db"],
            "Dorian intervals": d["steps"] == "W H W W W H W",
        }
    if case == "musokit-transpose":
        return {
            "input chords": d["original"] == ["C", "G", "Am", "F"],
            "up a whole tone": d["semitones"] == 2
            and d["transposed"] == ["D", "A", "Bm", "G"],
        }
    if case == "apished-datediff":
        delta = (dt.date(2028, 3, 1) - dt.date(2026, 11, 22)).days
        return {f"elapsed days equals {delta}": d == delta}
    if case == "apished-dayofweek":
        date = dt.date(2028, 3, 1)
        return {
            "weekday agrees with Python date": d.get("dayOfWeek")
            == date.strftime("%A"),
            "ISO weekday agrees": d.get("isoWeekday") == date.isoweekday(),
        }
    if case == "gumball-fallback-hour":
        zone = ZoneInfo("America/New_York")
        expected = {
            dt.datetime(2026, 11, 1, 1, 30, tzinfo=zone, fold=fold).astimezone(
                dt.timezone.utc
            )
            for fold in (0, 1)
        }
        return {
            "two distinct UTC instants agree with zoneinfo": set(
                map(dt.datetime.fromisoformat, d["candidates"])
            )
            == expected
        }
    if case == "gumball-spring-gap":
        observed = dt.datetime.fromisoformat(d["transitions"]["from"][0]["at"])
        actual = dt.datetime(2026, 3, 8, 7, tzinfo=dt.timezone.utc)
        return {
            "nonexistent local time has no instant or candidates": d["instant"] is None
            and d["candidates"] == [],
            f"reported transition differs by {(observed - actual).total_seconds():g} s from 07:00 UTC (minute precision only)": abs(
                (observed - actual).total_seconds()
            )
            < 60,
        }
    if case == "opencurrent-kr-holidays":
        dates = {r["date"]: r for r in d["holidays"]}
        return {
            f"{stamp}: provider lists {dates[stamp]['nameEn']} and weekday is {day}": dt.date.fromisoformat(
                stamp
            ).strftime("%A")
            == day
            for stamp, day in (("2026-10-03", "Saturday"), ("2026-10-09", "Friday"))
        }
    if case == "eancheck-isbn":
        digits = "978030640615"
        weighted = sum(int(x) * (1 if i % 2 == 0 else 3) for i, x in enumerate(digits))
        isbn = digits + str(-weighted % 10)
        return {
            "ISBN-13 digits and independently calculated check digit": isbn
            in text.replace("-", ""),
            "original ISBN-10 retained": "0306406152" in text.replace("-", ""),
        }
    if case.startswith("ghana-census-"):
        total = d["people"]
        reference = GHANA_NATIONAL if case.endswith("total") else ACCRA
        return {
            f"recorded subset {total:,} of official {reference:,}": 0
            < total
            < reference,
            f"recorded shortfall {reference - total:,} ({(reference - total) / reference:.2%})": sum(
                d["by_sex"].values()
            )
            == total,
            "age and schooling counts both sum to the reported subset": all(
                sum(d[k].values()) == total for k in ("by_age_band", "by_schooling")
            ),
            "provider describes five missing districts": "five districts"
            in d["coverage_note"].lower(),
        }
    if case == "humanities-check-claim":
        labels = {r["check"]: r["label"] for r in d["claims"]}
        return {
            "multiplication labeled proven": 17 * 23 == 391
            and labels.get("math") == "PROVEN",
            "composite labeled false": 7 * 13 == 91
            and labels.get("prime") == "FALSIFIED",
        }
    if case == "developerpoetry-read":
        return {
            "returned a bounded poem list": isinstance(d.get("poems"), list)
            and 0 < len(d["poems"]) <= 3
        }
    if case == "lemmapark-programs":
        return {
            "program names appear in the returned listing": all(
                s in text for s in ("erdos-open", "euclid-primes", "lean-ablations")
            )
        }
    raise ValueError(case)


def run(args):
    recipes = json.loads((HERE / "recipes.json").read_text())
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    rows = []
    with tempfile.TemporaryDirectory() as home:
        for r in recipes:
            alias = r["command"].split(" ", 1)[0]
            now = dt.datetime.now(dt.timezone.utc).isoformat()
            start = time.monotonic()
            raw, code = b"", -1
            try:
                proc = subprocess.run(
                    [
                        str(args.computer.resolve()),
                        "-mcp",
                        alias + "=" + r["endpoint"],
                        "-c",
                        r["command"],
                    ],
                    capture_output=True,
                    timeout=90,
                    env={"HOME": home, "PATH": os.defpath, "TZ": "UTC"},
                )
                raw = proc.stdout
                code = proc.returncode
                text = raw.decode("utf-8")
                try:
                    data = json.loads(text)
                except json.JSONDecodeError:
                    data = None
                checks = (
                    check(r["id"], data, text)
                    if code == 0
                    else {"guest exited successfully": False}
                )
            except (
                subprocess.TimeoutExpired,
                KeyError,
                TypeError,
                ValueError,
                AttributeError,
            ) as exc:
                checks = {type(exc).__name__ + ": response could not be checked": False}
            rows.append(
                {
                    **r,
                    "observed_utc": now,
                    "seconds": round(time.monotonic() - start, 2),
                    "stdout_sha256": hashlib.sha256(raw).hexdigest(),
                    "exit": code,
                    "checks": checks,
                    "passed": code == 0 and all(checks.values()),
                }
            )
            print(r["id"], rows[-1]["passed"], flush=True)
            if r != recipes[-1]:
                time.sleep(2.5)
    (out / "checks.json").write_text(
        json.dumps(rows, ensure_ascii=False, indent=2) + "\n"
    )
    return 0 if all(r["passed"] for r in rows) else 1


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--computer", required=True, type=Path)
    ap.add_argument("--output", required=True, type=Path)
    raise SystemExit(run(ap.parse_args()))
