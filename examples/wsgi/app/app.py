import base64
import re
import sqlite3
import sys
from contextlib import closing
from io import BytesIO

import pandas as pd
from flask import Flask, redirect, render_template, request, url_for

app = Flask(__name__)
app.config["DATABASE"] = "/data/lookups.sqlite3"


def connect():
    db = sqlite3.connect(app.config["DATABASE"])
    db.row_factory = sqlite3.Row
    db.execute("""
        CREATE TABLE IF NOT EXISTS lookups (
            id INTEGER PRIMARY KEY,
            repository TEXT NOT NULL,
            outline TEXT NOT NULL
        )
    """)
    return db


def render_chart(summary):
    import matplotlib

    matplotlib.use("Agg")
    import seaborn as sns
    from matplotlib.figure import Figure

    figure = Figure(figsize=(7.5, max(2, 1 + 0.4 * len(summary))), layout="constrained")
    axes = figure.subplots()
    sns.barplot(
        data=summary, x="mean_characters", y="repository",
        color="#28658c", errorbar=None, ax=axes,
    )
    axes.set(xlabel="Average outline length (characters)", ylabel="")
    with BytesIO() as output:
        figure.savefig(output, format="png", dpi=120)
        return base64.b64encode(output.getvalue()).decode("ascii")


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
        "index.html",
        lookups=lookups,
        summary=summary.to_dict("records"),
        chart=render_chart(summary.head(8)) if not summary.empty else None,
        platform=sys.platform,
    )


@app.post("/lookup")
def lookup():
    repository = request.form.get("repository", "").strip()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        return render_template("index.html", error="Enter a GitHub owner/repository."), 400

    from tools import ToolError, wiki

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
