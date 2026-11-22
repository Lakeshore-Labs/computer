"""Recompute the published counts from sample/survey.json and check sample/amendment-8.json against them.
usage: python3 -I check_counts.py   (no network; exits 1 on any mismatch)"""
import hashlib
import json
import re
from pathlib import Path
from urllib.parse import urlsplit

root = Path(__file__).resolve().parent / "sample"
rows = json.loads((root / "survey.json").read_text())
amend = json.loads((root / "amendment-8.json").read_text())
ids = [r["id"] for r in rows]
passed_urls = {u for r in rows if r["passed"] for u in r["endpoints"]}
seen, per_cohort = set(), {}
for n in sorted({r["pass_number"] for r in rows}):
    part = [r for r in rows if r["pass_number"] == n]
    urls = {u for r in part if r["passed"] for u in r["endpoints"]}
    per_cohort[n] = (len(part), sum(r["passed"] for r in part), len(urls - seen))
    seen |= urls
docs = amend["docs_server_pattern"]["ids"]
c3 = amend["cohort3"]
groups = {g: v for g, v in c3["groups"].items()}
c3_ids = [r["id"] for r in rows if r["pass_number"] == 3]
failures = []
def check(name, got, want):
    ok = got == want
    print(("ok   " if ok else "FAIL ") + name, got)
    if not ok:
        failures.append(name)
check("rows", len(rows), amend["final_counts"]["final_cases"])
check("unique ids", len(set(ids)), len(rows))
check("passed", sum(r["passed"] for r in rows), amend["final_counts"]["passed"])
check("endpoint URLs in passed cases", len(passed_urls), amend["final_counts"]["distinct_endpoint_urls_in_passed_cases"])
check("distinct hosts among them", len({urlsplit(u).netloc for u in passed_urls}), len(passed_urls))
check("per cohort (cases, passed, new URLs)", per_cohort, {1: (59, 56, 55), 2: (26, 25, 23), 3: (9, 9, 8)})
check("passed == all predicates", all(r["passed"] == all(c["passed"] for c in r["checks"]) and r["checks"] for r in rows), True)
check("docs-server cases present in survey", all(i in ids for i in docs), True)
check("docs-server cases / passed", (len(docs), sum(r["passed"] for r in rows if r["id"] in docs)),
      (amend["docs_server_pattern"]["cases"], amend["docs_server_pattern"]["passed"]))
check("failed docs-server cases", sorted(r["id"] for r in rows if r["id"] in docs and not r["passed"]),
      ["docs-agno", "docs-modular", "docs-windsurf"])
check("cohort 3 groups cover its cases", sorted(i for v in groups.values() for i in v if i != "story-lynx-two-catalogs") + ["story-lynx-two-catalogs"], sorted(c3_ids))
check("cohort 3 operator groups", len(groups), c3["operator_groups"])
att = amend["cohort3_attempts"]
check("attempt entries (runs)", sum(len(v) for v in att["cases"].values()), att["logged_runs"])
check("first-run passes", sum(v[0] == "pass" for v in att["cases"].values()), att["first_runs"]["passed"])
for path in sorted(root.glob("*/receipt.json")):
    receipt = json.loads(path.read_text())
    case = receipt["case"]
    row = next(r for r in rows if r["id"] == case)
    transcript = path.with_name("transcript.txt").read_bytes()
    check(case + " transcript digest", hashlib.sha256(transcript).hexdigest(), receipt["stdout_sha256"])
    check(case + " observation time", row["checked_utc"], receipt["observed_utc"])
    check(case + " recorded transcript", row.get("stdout_sha256"), receipt["stdout_sha256"])
    predicates = [{"check": pattern, "passed": result} for pattern, result in receipt["checks"].items()]
    check(case + " survey predicates", row["checks"], predicates)
    for pattern, result in receipt["checks"].items():
        check(case + " " + pattern, bool(re.search(pattern, transcript.decode(), re.MULTILINE)), result)
raise SystemExit(1 if failures else 0)
