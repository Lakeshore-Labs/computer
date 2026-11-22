"""Independent consumer workflow using Anki's real Python package + Rust backend.
usage: verify_workflow.py ED1.apkg ED2.apkg RECEIPT_DIR [--label NAME]
Everything runs in a throw-away temp collection (no profile, sync, GUI, accounts)."""

import json, os, re, shutil, sqlite3, sys, tempfile
from anki.buildinfo import version as ANKI_VERSION
from anki.collection import (
    Collection,
    DeckIdLimit,
    ExportAnkiPackageOptions,
    ImportAnkiPackageOptions,
    ImportAnkiPackageRequest,
)
from anki.import_export_pb2 import ImportAnkiPackageUpdateCondition as UC

ed1, ed2, rdir = [os.path.abspath(a) for a in sys.argv[1:4]]
os.makedirs(rdir, exist_ok=True)
TMP = tempfile.mkdtemp(prefix="moon-verify-")
checks, R = (
    [],
    {
        "anki_version": ANKI_VERSION,
        "editions": {"1": os.path.basename(ed1), "2": os.path.basename(ed2)},
    },
)


def check(name, ok, detail=None):
    checks.append({"check": name, "pass": bool(ok), "detail": detail})
    print(("PASS " if ok else "FAIL ") + name, "" if ok else f"-> {detail}")
    return ok


def clean(s):  # sanitise receipts
    return re.sub(
        r"/(?:private/)?(?:var|tmp|Users)/[^\s\"']*moon-verify-[^/\"'\s]*", "<TMP>", s
    )


def html_text(h):
    return re.sub(
        r"\s+", " ", re.sub(r"<[^>]+>", " ", re.sub(r"(?s)<style.*?</style>", "", h))
    ).strip()


def snapshot(col, deck_name="Four Moons"):
    notes = {}
    for nid in col.find_notes(""):
        n = col.get_note(nid)
        cards = []
        for c in n.cards():
            cards.append(
                {
                    "id": c.id,
                    "did": c.did,
                    "type": c.type,
                    "queue": c.queue,
                    "due": c.due,
                    "ivl": c.ivl,
                    "factor": c.factor,
                    "reps": c.reps,
                    "lapses": c.lapses,
                    "left": c.left,
                    "mod": c.mod,
                    "ord": c.ord,
                    "memory_state": (str(c.memory_state) if c.memory_state else None),
                    "question_html": c.question(),
                    "answer_html": c.answer(),
                    "question_text": html_text(c.question()),
                    "answer_text": html_text(c.answer()),
                }
            )
        notes[n.guid] = {
            "id": nid,
            "guid": n.guid,
            "mid": n.mid,
            "mod": n.mod,
            "fields": dict(n.items()),
            "cards": cards,
        }
    revlog = [
        dict(
            zip(
                (
                    "id",
                    "cid",
                    "usn",
                    "ease",
                    "ivl",
                    "lastIvl",
                    "factor",
                    "time",
                    "type",
                ),
                r,
            )
        )
        for r in col.db.all(
            "select id,cid,usn,ease,ivl,lastIvl,factor,time,type from revlog order by id"
        )
    ]
    return {
        "notes": notes,
        "revlog": revlog,
        "decks": {d.name: d.id for d in col.decks.all_names_and_ids()},
        "counts": {"notes": col.note_count(), "cards": col.card_count()},
    }


def do_import(col, path, label):
    opts = ImportAnkiPackageOptions(
        merge_notetypes=False,
        update_notes=UC.IMPORT_ANKI_PACKAGE_UPDATE_CONDITION_IF_NEWER,
        update_notetypes=UC.IMPORT_ANKI_PACKAGE_UPDATE_CONDITION_IF_NEWER,
        with_scheduling=True,
        with_deck_configs=True,
    )
    try:
        log = col.import_anki_package(
            ImportAnkiPackageRequest(package_path=path, options=opts)
        )
        s = {
            "new": len(log.log.new),
            "updated": len(log.log.updated),
            "duplicate": len(log.log.duplicate),
            "conflicting": len(log.log.conflicting),
            "first_field_match": len(log.log.first_field_match),
            "missing_notetype": len(log.log.missing_notetype),
            "missing_deck": len(log.log.missing_deck),
            "empty_first_field": len(log.log.empty_first_field),
        }
        R.setdefault("import_logs", {})[label] = s
        return s
    except Exception as e:  # surface version/format refusals loudly for HANDOFF
        R.setdefault("import_errors", {})[label] = (
            f"{type(e).__name__}: {clean(str(e))}"
        )
        check(
            f"import {label} accepted by Anki {ANKI_VERSION}",
            False,
            R["import_errors"][label],
        )
        return None


def finish(code):
    R["checks"] = checks
    R["all_pass"] = all(c["pass"] for c in checks) and code == 0
    open(f"{rdir}/workflow-receipt.json", "w").write(
        clean(json.dumps(R, indent=1, ensure_ascii=False))
    )
    shutil.rmtree(TMP, ignore_errors=True)
    print("ALL PASS" if R["all_pass"] else "FAILURES")
    sys.exit(0 if R["all_pass"] else 1)


path = os.path.join(TMP, "profile-isolated", "collection.anki2")
os.makedirs(os.path.dirname(path))
col = Collection(path)
# ---- 1. import edition 1
s1 = do_import(col, ed1, "edition-1")
if s1 is None:
    col.close()
    finish(1)
A = snapshot(col)
R["after_edition1"] = A
check(
    "edition 1: 4 notes / 4 cards", A["counts"] == {"notes": 4, "cards": 4}, A["counts"]
)
# media Anki imported (for the offline study desk) + its hash
mdir = os.path.join(rdir, "media-from-anki")
os.makedirs(mdir, exist_ok=True)
import hashlib

R["media"] = {}
for fn in sorted(os.listdir(col.media.dir())):
    if fn.startswith("_") or fn.startswith("."):
        continue
    shutil.copy(os.path.join(col.media.dir(), fn), os.path.join(mdir, fn))
    R["media"][fn] = hashlib.sha256(
        open(os.path.join(mdir, fn), "rb").read()
    ).hexdigest()
eu_guid = next(
    g
    for g, n in A["notes"].items()
    if any(v.strip() == "Europa" for v in n["fields"].values())
    or "Europa" in list(n["fields"].values())[0]
)
eu1 = A["notes"][eu_guid]
cid = eu1["cards"][0]["id"]
check(
    "Europa card is new before review",
    eu1["cards"][0]["type"] == 0,
    eu1["cards"][0]["type"],
)
# ---- 2. review Europa via the real v3 scheduler (Easy)
deck_id = eu1["cards"][0]["did"]
col.decks.set_current(deck_id)
# Anki's v3 scheduler only accepts an answer for the card at the TOP of the queue. To review Europa
# specifically, temporarily suspend the other cards (receipted), answer, then unsuspend them.
others_cids = [
    c["id"] for g, n in A["notes"].items() if g != eu_guid for c in n["cards"]
]
col.sched.suspend_cards(others_cids)
qc = col.sched.get_queued_cards(fetch_limit=10)
target = qc.cards[0] if qc.cards else None
check(
    "Europa is the top card of Anki's scheduler queue",
    target is not None and target.card.id == cid,
    [q.card.id for q in qc.cards],
)
from anki.scheduler_pb2 import CardAnswer

card = col.get_card(cid)
card.start_timer()
col.sched.answer_card(
    col.sched.build_answer(card=card, states=target.states, rating=CardAnswer.EASY)
)
col.sched.unsuspend_cards(others_cids)
R["review_method"] = (
    "suspend 3 other cards -> v3 get_queued_cards/answer_card(Easy) on Europa -> unsuspend"
)
B = snapshot(col)
R["after_review"] = B
ec = B["notes"][eu_guid]["cards"][0]
check(
    "review log has exactly 1 entry for Europa (rating Easy=4)",
    len(B["revlog"]) == 1
    and B["revlog"][0]["cid"] == cid
    and B["revlog"][0]["ease"] == 4,
    B["revlog"],
)
check(
    "Europa now a review card (type=2, ivl>0)",
    ec["type"] == 2 and ec["ivl"] > 0,
    {k: ec[k] for k in ("type", "queue", "ivl", "due", "reps")},
)
col.close()
# ---- 3. close / reopen, import edition 2
col = Collection(path)
s2 = do_import(col, ed2, "edition-2")
if s2 is None:
    col.close()
    finish(1)
check(
    "edition 2 updated exactly 1 note, 0 new", s2["updated"] == 1 and s2["new"] == 0, s2
)
C = snapshot(col)
R["after_edition2"] = C


def verify_final(S, tag):
    check(
        f"{tag}: still 4 notes / 4 cards",
        S["counts"] == {"notes": 4, "cards": 4},
        S["counts"],
    )
    check(
        f"{tag}: same GUIDs, note ids, model id, deck id",
        {g: (n["id"], n["mid"]) for g, n in S["notes"].items()}
        == {g: (n["id"], n["mid"]) for g, n in A["notes"].items()}
        and {c["did"] for n in S["notes"].values() for c in n["cards"]} == {deck_id},
        None,
    )
    check(
        f"{tag}: Europa card id preserved",
        S["notes"][eu_guid]["cards"][0]["id"] == cid,
        None,
    )
    check(
        f"{tag}: old review log retained byte-for-byte",
        S["revlog"] == B["revlog"],
        S["revlog"],
    )
    sched_keys = (
        "type",
        "queue",
        "due",
        "ivl",
        "factor",
        "reps",
        "lapses",
        "left",
        "memory_state",
    )
    check(
        f"{tag}: Europa scheduling retained",
        {k: S["notes"][eu_guid]["cards"][0][k] for k in sched_keys}
        == {k: ec[k] for k in sched_keys},
        None,
    )
    others = [g for g in A["notes"] if g != eu_guid]
    check(
        f"{tag}: 3 other notes unchanged (fields + cards + sched)",
        all(S["notes"][g] == B["notes"][g] for g in others),
        None,
    )
    R.setdefault("others_diff_vs_post_review", {})[tag] = {
        A["notes"][g]["fields"].get(list(A["notes"][g]["fields"])[0]): {
            "note": [
                k
                for k in S["notes"][g]
                if k != "cards" and S["notes"][g][k] != B["notes"][g][k]
            ],
            "card": [
                k
                for k in S["notes"][g]["cards"][0]
                if S["notes"][g]["cards"][0][k] != B["notes"][g]["cards"][0][k]
            ],
        }
        for g in others
    }
    check(
        f"{tag}: other notes' fields identical to edition-1 import and still new/unsuspended",
        all(
            S["notes"][g]["fields"] == A["notes"][g]["fields"]
            and S["notes"][g]["cards"][0]["queue"] == 0
            and S["notes"][g]["cards"][0]["type"] == 0
            for g in others
        ),
        None,
    )
    changed = [
        f for f in eu1["fields"] if S["notes"][eu_guid]["fields"][f] != eu1["fields"][f]
    ]
    check(f"{tag}: Europa changed fields", len(changed) >= 1, changed)
    R.setdefault("europa_changed_fields", {})[tag] = changed
    ans = S["notes"][eu_guid]["cards"][0]["answer_text"]
    check(
        f"{tag}: new Europa answer visible in Anki-rendered answer",
        "0.30 km" in ans and "±" in ans,
        ans,
    )
    check(
        f"{tag}: old answer lacked ±0.30 km",
        "0.30" not in eu1["cards"][0]["answer_text"],
        None,
    )


verify_final(C, "after import 2")
col.close()
col = Collection(path)
D = snapshot(col)
R["after_reopen"] = D
verify_final(D, "after close/reopen")
# raw sqlite read-only cross check
col.close()
db = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
rl = db.execute("select count(*), min(cid) from revlog").fetchone()
check("raw sqlite: revlog 1 row for Europa card", rl == (1, cid), rl)
db.close()
# ---- 4. export updated deck as portable .apkg
col = Collection(path)
outp = os.path.join(rdir, "updated-deck-export.apkg")
n_exp = col.export_anki_package(
    out_path=outp,
    options=ExportAnkiPackageOptions(
        with_scheduling=True, with_deck_configs=True, with_media=True, legacy=False
    ),
    limit=DeckIdLimit(deck_id),
)
col.close()
check("exported updated deck .apkg (note count)", n_exp == 4, n_exp)
R["export"] = {
    "file": "updated-deck-export.apkg",
    "bytes": os.path.getsize(outp),
    "notes": n_exp,
}
# round trip into brand-new collection
p2 = os.path.join(TMP, "roundtrip", "collection.anki2")
os.makedirs(os.path.dirname(p2))
c2 = Collection(p2)
if do_import(c2, outp, "export-roundtrip") is not None:
    E = snapshot(c2)
    R["roundtrip"] = E
    check(
        "round-trip: 4 notes/4 cards, revlog 1, Europa sched retained, new answer",
        E["counts"] == {"notes": 4, "cards": 4}
        and len(E["revlog"]) == 1
        and E["notes"][eu_guid]["cards"][0]["ivl"] == ec["ivl"]
        and "0.30 km" in E["notes"][eu_guid]["cards"][0]["answer_text"],
        E["counts"],
    )
c2.close()
# UI payload: genuine Anki-rendered Q/A for both editions + observed scheduling
R["ui"] = {
    "europa_guid": eu_guid,
    "review": {
        "rating": "Easy",
        "card_id": cid,
        "revlog": B["revlog"][0],
        "before": {
            k: eu1["cards"][0][k]
            for k in ("type", "queue", "due", "ivl", "factor", "reps")
        },
        "after": {k: ec[k] for k in ("type", "queue", "due", "ivl", "factor", "reps")},
    },
}
finish(0)
