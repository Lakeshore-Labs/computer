"""Runs inside the NEW read-only guest (CPython on WASI, standard library only).

Reads both restored editions from /mailbag (read-only), checks them against the sums and manifests the receiving store
wrote, derives each raw CID again with the standard library, reads the book and the cards, and writes the practical
decision to /out/decision.json.
"""
import base64, hashlib, json, re, sys, zipfile


def raw_cid(data):
    """CIDv1, raw codec (0x55), sha2-256 (0x12, 32 bytes), multibase base32 lower: built from the specification's bytes."""
    return "b" + base64.b32encode(bytes([1, 0x55, 0x12, 0x20]) + hashlib.sha256(data).digest()).decode().lower().rstrip("=")


def book_info(path):
    z = zipfile.ZipFile(path)
    opf = z.read("EPUB/package.opf").decode()
    page = z.read("EPUB/measure.xhtml").decode()
    return {
        "mimetype": z.read("mimetype").decode(),
        "title": re.search(r"<dc:title>(.*?)</dc:title>", opf).group(1),
        "chapters": len(re.findall(r"<itemref ", opf)),
        "moons": [{"name": a, "radius_km": b} for a, b in re.findall(r'<th scope="row">(.*?)</th><td>(.*?)</td>', page)],
        "zip_ok": z.testzip() is None,
    }


def card_fields(text):
    return dict(ln.split(": ", 1) for ln in text.split("\n") if re.match(r"^(Edition|Book|Inside|Where|When|Changed): ", ln))


checks = []


def check(title, ok, detail=""):
    checks.append({"title": title, "pass": bool(ok), "detail": detail})
    print(("PASS " if ok else "FAIL ") + title + (f"  [{detail}]" if detail else ""))


editions = {}
for n in (1, 2):
    base = f"/mailbag/edition-{n}"
    meta = json.load(open(f"{base}.manifest.json"))
    root = open(f"{base}.ROOT").read().strip()
    sums = {name: digest for digest, name in (ln.split() for ln in open(f"{base}.SHA256SUMS").read().splitlines())}
    files = {}
    for f in meta["files"]:
        data = open(f"{base}/{f['name']}", "rb").read()
        sha = hashlib.sha256(data).hexdigest()
        files[f["name"]] = {"bytes": len(data), "sha256": sha, "cid": f["cid"], "data": data}
        check(f"edition {n}: {f['name']} sha256 equals the digest inside its CID", sha == sums[f["name"]] == f["sha256"], sha[:16])
        check(f"edition {n}: {f['name']} raw CID derived here equals the manifest's", raw_cid(data) == f["cid"], f["cid"])
        check(f"edition {n}: {f['name']} size equals the manifest's", len(data) == f["size"] if "size" in f else len(data) == f["bytes"], str(len(data)))
    card = card_fields(files["reading-card.md"]["data"].decode())
    editions[n] = {"root": root, "meta_edition": meta["edition"], "files": files, "card": card}
    check(f"edition {n}: the card says edition {n}", card["Edition"] == str(n) == str(meta["edition"]))

b1, b2 = editions[1]["files"]["four-moons.epub"], editions[2]["files"]["four-moons.epub"]
check("the book is the same bytes in both editions", b1["data"] == b2["data"], b1["sha256"][:16])
check("the book has one CID in both editions", b1["cid"] == b2["cid"], b1["cid"])
check("the two roots differ", editions[1]["root"] != editions[2]["root"])
check("the two cards differ", editions[1]["files"]["reading-card.md"]["cid"] != editions[2]["files"]["reading-card.md"]["cid"])

info = book_info("/mailbag/edition-2/four-moons.epub")
check("the EPUB opens as a ZIP with the EPUB mimetype", info["zip_ok"] and info["mimetype"] == "application/epub+zip")
check("the book has 3 chapters and 4 moons", info["chapters"] == 3 and len(info["moons"]) == 4, f"{info['chapters']} chapters")

c1, c2 = editions[1]["card"], editions[2]["card"]
changed = [k for k in ("Where", "When") if c1[k] != c2[k]]
check("exactly one meeting field changed, and it is the time", changed == ["When"], ",".join(changed))
decision = {
    "use_edition": 2,
    "where": c2["Where"],
    "when": c2["When"],
    "was_when": c1["When"],
    "was_where": c1["Where"],
    "changed_fields": changed,
    "changed_note": c2.get("Changed"),
}
book = {"title": info["title"], "chapters": info["chapters"], "moons": info["moons"], "bytes": b1["bytes"], "sha256": b1["sha256"], "cid": b1["cid"]}
out = {
    "decision": decision,
    "book": book,
    "editions": {
        str(n): {"root": e["root"], "card": e["card"], "card_cid": e["files"]["reading-card.md"]["cid"],
                 "card_bytes": e["files"]["reading-card.md"]["bytes"], "card_sha256": e["files"]["reading-card.md"]["sha256"],
                 "card_text": e["files"]["reading-card.md"]["data"].decode()}
        for n, e in editions.items()
    },
    "checks": checks,
    "all_pass": all(c["pass"] for c in checks),
}
json.dump(out, open("/out/decision.json", "w"), indent=2)
open("/out/decision.json", "a").write("\n")
print()
print(f"DECISION: meet at {decision['where']}, {decision['when']} (was {decision['was_when']}); changed: {', '.join(changed)}")
print(f"{sum(c['pass'] for c in checks)}/{len(checks)} guest checks passed")
sys.exit(0 if out["all_pass"] else 1)
