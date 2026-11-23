"""Build the four-cohort workbook from recorded checks without contacting any server."""

import collections
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
sample = HERE / "sample"
rows = json.loads((HERE.parent / "sample/survey.json").read_text())
fourth = json.loads((sample / "checks.json").read_text())
for r in fourth:
    rows.append({
        "pass_number": 4,
        "id": r["id"],
        "task": r["title"],
        "provider": r["provider"],
        "endpoints": [r["endpoint"]],
        "passed": r["passed"],
        "category": "Reference desk",
        "checks": [{"check": k, "passed": v} for k, v in r["checks"].items()],
        "checked_utc": r["observed_utc"],
        "stdout_sha256": r["stdout_sha256"],
        "note": "Computer shell: " + r["command"] + " · stdout SHA-256 " + r["stdout_sha256"],
    })

assert len(rows) == len({r["id"] for r in rows})
assert all(r["checks"] and r["passed"] == all(c["passed"] for c in r["checks"]) for r in rows)
passed = [r for r in rows if r["passed"]]
counts = {
    "cases": len(rows),
    "passed": len(passed),
    "distinct_passing_urls": len({u for r in passed for u in r["endpoints"]}),
    "cases_per_cohort": dict(sorted(collections.Counter(r["pass_number"] for r in rows).items())),
    "fourth_cohort_urls": sorted({r["endpoint"] for r in fourth if r["passed"]}),
}
assert (counts["cases"], counts["passed"], counts["distinct_passing_urls"]) == (107, 103, 95)
(sample / "counts.json").write_text(json.dumps(counts, indent=2) + "\n")
template = (HERE.parent / "fieldwork.html").read_text()
insert = (HERE / "reference.html").read_text()
template = template.replace('<div class="controls">', insert + '\n<div class="controls">')
template = template.replace('class="shortcuts">', 'class="shortcuts"><button data-query="ghana-census-accra">Greater Accra</button><button data-query="gumball-spring-gap">Missing hour</button>')
html = template.replace("{{SURVEY}}", json.dumps(rows, ensure_ascii=False, separators=(",", ":")).replace("<", r"\u003c"))
(sample / "four-cohorts.html").write_text(html)
print(json.dumps(counts, indent=2))
