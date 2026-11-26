import re
import sqlite3
import sys
from contextlib import closing

import pandas as pd
from flask import Flask, redirect, render_template, request, url_for

from tools import ToolError, wiki

app = Flask(__name__)


def connect():
    db = sqlite3.connect("/data/lookups.sqlite3")
    db.row_factory = sqlite3.Row
    db.execute("""
        CREATE TABLE IF NOT EXISTS lookups (
            id INTEGER PRIMARY KEY,
            repository TEXT NOT NULL,
            outline TEXT NOT NULL
        )
    """)
    return db


@app.get("/")
def index():
    with closing(connect()) as db:
        lookups = db.execute(
            "SELECT repository, outline FROM lookups ORDER BY id DESC LIMIT 5"
        ).fetchall()
        history = pd.read_sql_query(
            "SELECT repository, length(outline) AS characters FROM lookups", db
        )
    summary = (
        history.groupby("repository", as_index=False)
        .agg(lookups=("characters", "size"), mean_characters=("characters", "mean"))
        .sort_values(["lookups", "repository"], ascending=[False, True])
    )
    return render_template(
        "index.html", lookups=lookups, summary=summary.to_dict("records"), platform=sys.platform
    )


@app.post("/lookup")
def lookup():
    repository = request.form.get("repository", "").strip()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        return render_template("index.html", error="Enter a GitHub owner/repository."), 400

    try:
        outline = wiki.read_wiki_structure(repo_name=repository)
    except ToolError as error:
        app.logger.warning("Repository outline lookup failed: %s", error)
        return render_template("index.html", error="The outline server could not load that repository."), 502

    with closing(connect()) as db, db:
        db.execute(
            "INSERT INTO lookups (repository, outline) VALUES (?, ?)",
            (repository, str(outline)),
        )
    return redirect(url_for("index"), code=303)
