"""Build the offline study desk from a verifier receipt. usage: build_study_desk.py RECEIPT.json OUT.html"""

import json, re, sys

r = json.load(open(sys.argv[1]))
A, C = r["after_edition1"], r["after_edition2"]


def back(
    card,
):  # answer text with the question part removed (the answer template starts with FrontSide)
    q, a = card["question_text"], card["answer_text"]
    return a[len(q) :].strip() if a.startswith(q) else a


cards = []
for g, n in A["notes"].items():
    c1, c2 = n["cards"][0], C["notes"][g]["cards"][0]
    ed = {}
    for k, c in (("1", c1), ("2", c2)):
        ed[k] = {
            x: c[x]
            for x in ("question_html", "answer_html", "question_text", "answer_text")
        }
        ed[k]["back_text"] = back(c)
    cards.append({"guid": g, "title": list(n["fields"].values())[0], "ed": ed})
data = {
    "anki_version": r["anki_version"],
    "cards": cards,
    "ui": r["ui"],
    "checks": r["checks"],
    "import2": r["import_logs"]["edition-2"],
    "changed": r["europa_changed_fields"]["after close/reopen"],
    "counts": {
        k: {
            "notes": r[k]["counts"]["notes"],
            "cards": r[k]["counts"]["cards"],
            "revlog": len(r[k]["revlog"]),
        }
        for k in ("after_edition1", "after_edition2")
    },
}
t = open(__file__.rsplit("/", 1)[0] + "/study_desk_template.html").read()
js = json.dumps(data, ensure_ascii=False).replace("</", "<\\/")
open(sys.argv[2], "w").write(
    t.replace("__DATA__", js).replace("__VER__", r["anki_version"])
)
print("wrote", sys.argv[2])
