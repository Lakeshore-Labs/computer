set -eo pipefail
pdftotext /source/operations.pdf extracted-pages.txt
python3 - <<'PY'
import sqlite3
from pathlib import Path

pages = [
    p.strip() for p in Path("extracted-pages.txt").read_text().split("\f") if p.strip()
]
with sqlite3.connect("brief-search.db") as db:
    db.execute("DROP TABLE IF EXISTS pages")
    db.execute("CREATE VIRTUAL TABLE pages USING fts5(body)")
    db.executemany("INSERT INTO pages(body) VALUES(?)", [(p,) for p in pages])
    for page, hit in db.execute(
        "SELECT rowid,snippet(pages,0,'[',']','…',12) FROM pages WHERE pages MATCH ?",
        ("detail",),
    ):
        print("Page", page, ":", " ".join(hit.split()))
PY
