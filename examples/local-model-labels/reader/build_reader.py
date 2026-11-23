#!/usr/bin/env python3
"""Build the offline wardrobe pickup print desk reader (v2).

usage: build_reader.py RUN_DIR [RUN_DIR ...] OUT_DIR

Each run directory holds receipt.json and desk/{label.txt,inspect.json}, plus
desk/correct.json when the desk had to correct the draft, and desk/ticket.zpl
whenever a queue file exists. A run is either:

  * accepted at inspection (correct.json absent, queue file already present), or
  * held at inspection, then corrected (correct.json present).

Everything shown is derived from those files. Output is one self-contained
OUT_DIR/index.html built from src/ (no network, no external assets).
"""

import base64
import hashlib
import json
import os
import re
import sys

SRC = os.path.join(os.path.dirname(os.path.abspath(__file__)), "src")

RIBBON_COLORS = {
    "fern": "#4f7d4a",
    "copper": "#b8693a",
    "ink": "#2b3a55",
    "rose": "#c25a74",
    "sage": "#8aa17d",
    "amber": "#d9a02b",
    "slate": "#5b6b7a",
    "plum": "#7b4b7a",
}


class BuildError(Exception):
    pass


def slug(text):
    return re.sub(r"[^a-z0-9-]+", "-", str(text).lower()).strip("-")


def read_bytes(run, *parts, required=True):
    path = os.path.join(run, *parts)
    if not os.path.isfile(path):
        if required:
            raise BuildError(
                "%s: missing %s" % (os.path.basename(run), "/".join(parts))
            )
        return None
    with open(path, "rb") as handle:
        return handle.read()


def read_json(run, *parts, required=True):
    raw = read_bytes(run, *parts, required=required)
    return None if raw is None else json.loads(raw)


def check_gate(name, stage, gate, ticket_sha):
    """A queued gate must carry the ticket's sha; an unqueued one must not."""
    if gate["queue_file_exists"]:
        if ticket_sha is None or gate["zpl_sha256"] != ticket_sha:
            raise BuildError(
                "%s: %s ZPL sha-256 does not match ticket.zpl" % (name, stage)
            )
    elif gate["zpl_sha256"] is not None:
        raise BuildError("%s: %s has a sha but no queue file" % (name, stage))


def stage_record(key, title, gate):
    return {
        "key": key,
        "title": title,
        "decision": gate["decision"],
        "queued": bool(gate["queue_file_exists"]),
    }


def load_run(run):
    name = slug(os.path.basename(os.path.normpath(run)))
    receipt = read_json(run, "receipt.json")
    label = read_bytes(run, "desk", "label.txt").decode("utf-8")
    inspect = read_json(run, "desk", "inspect.json")
    correct = read_json(run, "desk", "correct.json", required=False)
    ticket = read_bytes(run, "desk", "ticket.zpl", required=False)

    if inspect["draft"] != label:
        raise BuildError("%s: label.txt differs from the inspection draft" % name)
    if correct and correct["draft"] != label:
        raise BuildError("%s: label.txt differs from the correction draft" % name)

    ticket_sha = hashlib.sha256(ticket).hexdigest() if ticket is not None else None
    check_gate(name, "inspect", inspect, ticket_sha)
    final = correct or inspect
    if correct:
        check_gate(name, "issue", correct, ticket_sha)
    if (
        inspect["queue_file_exists"] or (correct and correct["queue_file_exists"])
    ) is False and ticket:
        raise BuildError("%s: ticket.zpl present but no gate queued it" % name)

    # The receipt must agree with the desk files it summarises.
    if receipt.get("initial_gate", inspect)["decision"] != inspect["decision"]:
        raise BuildError("%s: receipt initial gate disagrees with inspect.json" % name)
    if receipt.get("final_gate", final)["decision"] != final["decision"]:
        raise BuildError("%s: receipt final gate disagrees with desk files" % name)

    source = final["source_label"]
    if ticket is not None and source.encode("utf-8") not in re.findall(
        rb"\^FD(.*?)\^FS", ticket
    ):
        raise BuildError("%s: source label not found in ZPL" % name)

    is_control = "lookup" not in receipt and "continued" not in receipt
    tools = []
    for part in ("lookup", "continued"):
        tools += [call["name"] for call in receipt.get(part, {}).get("tool_calls", [])]

    shelf, _, ribbon = source.partition(" ")
    ribbon_hex = RIBBON_COLORS.get(ribbon.lower())
    if ribbon_hex is None:
        hue = int(hashlib.sha256(ribbon.encode("utf-8")).hexdigest()[:4], 16) % 360
        ribbon_hex = "hsl(%d 40%% 42%%)" % hue

    stages = [stage_record("inspect", "Inspect", inspect)]
    stages.append(
        stage_record("issue", "Issue", correct)
        if correct
        else {"key": "issue", "title": "Issue", "decision": None, "queued": False}
    )
    return {
        "id": name,
        "control": is_control,
        "shelf": shelf,
        "ribbon": ribbon,
        "ribbonHex": ribbon_hex,
        "model": None if is_control else receipt["model"],
        "modelDigest": None if is_control else receipt["model_digest"],
        "controlNote": receipt.get("classification") if is_control else None,
        "tools": tools,
        "saved": label,
        "source": source,
        "stages": stages,
        "ticket": None
        if ticket is None
        else {
            "name": "wardrobe-%s-%s.zpl" % (name, slug(source)),
            "sha256": ticket_sha,
            "b64": base64.b64encode(ticket).decode("ascii"),
        },
    }


def read_src(name):
    with open(os.path.join(SRC, name), encoding="utf-8") as handle:
        return handle.read()


def build(runs, out_dir):
    jobs = [load_run(run) for run in runs]
    ids = [job["id"] for job in jobs]
    if len(set(ids)) != len(ids):
        raise BuildError(
            "run directories must have distinct names: %s" % ", ".join(ids)
        )
    data = json.dumps(jobs, ensure_ascii=False, indent=1).replace("</", "<\\/")
    html = read_src("index.template.html")
    for token, value in (
        ("/*CSS*/", read_src("reader.css")),
        ("/*JS*/", read_src("reader.js")),
        ("/*DATA*/", data),
    ):
        html = html.replace(token, value)
    os.makedirs(out_dir, exist_ok=True)
    with open(os.path.join(out_dir, "index.html"), "w", encoding="utf-8") as handle:
        handle.write(html)
    return ids


def main(argv):
    if len(argv) < 3:
        sys.exit("usage: build_reader.py RUN_DIR [RUN_DIR ...] OUT_DIR")
    try:
        ids = build(argv[:-1], argv[-1])
    except BuildError as error:
        sys.exit("build_reader: %s" % error)
    print("wrote index.html with runs: %s" % ", ".join(ids))


if __name__ == "__main__":
    main(sys.argv[1:])
