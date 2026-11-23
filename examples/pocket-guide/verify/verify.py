#!/usr/bin/env python3
"""Independent print verifier. Usage: verify.py --pdf FILE --out NEW_DIR [--wait SECONDS]
Needs Poppler (pdfinfo, pdftotext, pdftoppm) and swift (Apple Vision) on macOS."""

import argparse, json, os, re, subprocess, sys, time, hashlib, struct

HERE = os.path.dirname(os.path.abspath(__file__))
MAIN = "https://github.com/Lakeshore-Labs/computer/pull/56"
TITLES = [
    "Ferry dispatch",
    "Venice in pixels",
    "A rehearsal score",
    "An island mailbag",
    "A radio cue",
    "A lunch delivery",
]
PR_RE = re.compile(r"^https://github\.com/Lakeshore-Labs/computer/pull/\d+/?$")


def run(*a):
    return subprocess.run(a, capture_output=True, text=True, check=False)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--pdf", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--wait", type=int, default=0, help="seconds to wait for PDF")
    a = ap.parse_args()
    end = time.time() + a.wait
    while not os.path.isfile(a.pdf) or os.path.getsize(a.pdf) == 0:
        if time.time() > end:
            sys.exit("PDF not found: " + os.path.basename(a.pdf))
        time.sleep(5)
    last = -1
    while os.path.getsize(a.pdf) != last:  # wait until size stable
        last = os.path.getsize(a.pdf)
        time.sleep(2)
    os.makedirs(a.out, exist_ok=False)
    checks = {}

    def chk(k, ok, detail):
        checks[k] = {"pass": bool(ok), "detail": detail}

    info = run("pdfinfo", "-box", a.pdf).stdout
    pages = int(re.search(r"^Pages:\s+(\d+)", info, re.M).group(1))
    m = re.search(r"^Page size:\s+([\d.]+) x ([\d.]+) pts", info, re.M)
    w, h = float(m.group(1)), float(m.group(2))
    chk("page_count", pages == 1, pages)
    chk(
        "a4_landscape",
        abs(w - 841.89) < 1.5 and abs(h - 595.28) < 1.5 and w > h,
        f"{w} x {h} pt = {w / 72 * 25.4:.1f} x {h / 72 * 25.4:.1f} mm",
    )
    rot = re.search(r"^Page rot:\s+(\d+)", info, re.M)
    chk("no_rotation", not rot or rot.group(1) == "0", rot.group(1) if rot else "0")

    text = run("pdftotext", "-layout", a.pdf, "-").stdout
    open(os.path.join(a.out, "text.txt"), "w").write(text)
    flat = re.sub(r"\s+", " ", text)
    for t in TITLES:
        chk("title: " + t, t in flat, "found" if t in flat else "missing")

    urls = []
    for l in run("pdfinfo", "-url", a.pdf).stdout.splitlines():
        mm = re.match(r"\s*\d+\s+Annotation\s+(https?://\S+)", l)
        if mm:
            urls.append(mm.group(1))
    prs = sorted({u for u in urls if PR_RE.match(u) and u.rstrip("/") != MAIN})
    chk("main_guide_link", MAIN in [u.rstrip("/") for u in urls], MAIN)
    chk(
        "six_project_pr_links_plus_film",
        len(prs) >= 7,
        f"{len(prs)} distinct non-guide PR links: {prs}",
    )
    wanted = {
        f"https://github.com/Lakeshore-Labs/computer/pull/{n}"
        for n in (16, 47, 53, 56, 59, 62, 65, 66)
    }
    chk("exact_link_targets", set(urls) == wanted and len(urls) == 8, sorted(urls))
    chk("film_text_present", "film" in flat.lower(), "film link text is present")
    chk(
        "link_total",
        len(set(urls)) >= 8,
        f"{len(set(urls))} distinct, {len(urls)} total annotations",
    )

    base = os.path.join(a.out, "page")
    r = run("pdftoppm", "-r", "144", "-png", "-singlefile", a.pdf, base)
    png = base + ".png"
    ok = r.returncode == 0 and os.path.isfile(png)
    dims = None
    if ok:
        with open(png, "rb") as f:
            d = f.read(24)
            dims = struct.unpack(">II", d[16:24])
    chk(
        "raster_144dpi",
        ok and abs(dims[0] - round(w * 2)) <= 1 and abs(dims[1] - round(h * 2)) <= 1,
        dims,
    )
    os.rename(png, os.path.join(a.out, "pocket-guide-144dpi.png"))
    png = os.path.join(a.out, "pocket-guide-144dpi.png")

    q = run("swift", os.path.join(HERE, "qr_vision.swift"), png)
    try:
        res = json.loads(q.stdout)
    except Exception:
        res = []
    payloads = [x["payload"] for x in res]
    chk(
        "qr_decoded_exactly",
        payloads == [MAIN],
        {
            "decoder": "Apple Vision VNDetectBarcodesRequest on full-page 144dpi PNG",
            "payloads": payloads,
            "stderr": q.stderr[-200:] if not res else "",
        },
    )

    receipt = {
        "pdf": os.path.basename(a.pdf),
        "pdf_bytes": os.path.getsize(a.pdf),
        "pdf_sha256": hashlib.sha256(open(a.pdf, "rb").read()).hexdigest(),
        "png": os.path.basename(png),
        "png_px": dims,
        "urls": sorted(set(urls)),
        "checks": checks,
        "all_pass": all(c["pass"] for c in checks.values()),
    }
    json.dump(receipt, open(os.path.join(a.out, "receipt.json"), "w"), indent=2)
    for k, c in checks.items():
        print(("PASS " if c["pass"] else "FAIL ") + k + " - " + str(c["detail"]))
    print("ALL PASS" if receipt["all_pass"] else "FAILURES")
    sys.exit(0 if receipt["all_pass"] else 1)


main()
