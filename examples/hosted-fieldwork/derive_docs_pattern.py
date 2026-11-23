"""Derive the docs-server case list from local raw capture JSON files (file name = case id, field "script").

usage: derive_docs_pattern.py SURVEY_JSON RAW_DIR [RAW_DIR ...]
Predicate: a script line starts with `<alias> query-docs-filesystem*` (a generated docs-server tool call).
Prints the matching survey case ids, how many passed, and how many of them also call a search-* tool."""
import json, re, sys
from pathlib import Path

survey = {r["id"]: r for r in json.loads(Path(sys.argv[1]).read_text())}
hits, with_search = set(), set()
for raw_dir in sys.argv[2:]:
    for path in Path(raw_dir).glob("*.json"):
        if path.stem not in survey:
            continue
        script = json.loads(path.read_text()).get("script", "")
        if re.search(r"^\s*\w+ query-docs-filesystem[\w-]*\b", script, re.M):
            hits.add(path.stem)
            if re.search(r"^\s*\w+ search-[\w-]+", script, re.M):
                with_search.add(path.stem)
print(json.dumps({"cases": len(hits), "passed": sum(survey[i]["passed"] for i in hits),
                  "also_call_search_tool": len(with_search), "ids": sorted(hits)}, indent=1))
