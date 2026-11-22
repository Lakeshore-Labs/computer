#!/usr/bin/env python3
"""Drive the built reader in real Chromium (Playwright), desktop + 390px.
Checks: offline (no network), selection, real WebAudio playback (AudioContext running, clock
advancing, analyser sees signal, notes highlight), pause/resume/stop, click-to-seek to the end,
downloaded files byte-identical (sha256) to the original parts, no horizontal overflow. Saves stills.

usage: verify_browser.py --site site --parts ../run1/parts --out proof/browser [--tag run1]
"""
import argparse, hashlib, json, os
from playwright.sync_api import sync_playwright

def sha(b): return hashlib.sha256(b).hexdigest()

def run(pw, site, parts, out, tag, name, vw, vh):
    res, checks = {}, []
    def chk(n, ok, d=""): checks.append(dict(name=f"[{name}] {n}", ok=bool(ok), detail=str(d)))
    b = pw.chromium.launch(args=["--autoplay-policy=no-user-gesture-required"])
    ctx = b.new_context(viewport=dict(width=vw, height=vh), accept_downloads=True, device_scale_factor=2 if vw < 500 else 1)
    pg = ctx.new_page()
    reqs = []
    pg.on("request", lambda r: reqs.append(r.url))
    errs = []; pg.on("pageerror", lambda e: errs.append(str(e))); pg.on("console", lambda m: errs.append(m.text) if m.type == "error" else None)
    pg.goto("file://" + os.path.abspath(f"{site}/index.html")); pg.wait_for_timeout(600)
    shot = lambda n: pg.screenshot(path=f"{out}/{tag}-{name}-{n}.png", full_page=True)
    chk("offline: every request is file://", all(u.startswith(("file:", "data:")) for u in reqs), len(reqs))
    chk("no page/console errors", not errs, errs)
    chk("no horizontal overflow", pg.evaluate("document.documentElement.scrollWidth<=innerWidth"), pg.evaluate("[document.documentElement.scrollWidth,innerWidth]"))
    vis = pg.evaluate("[...document.querySelectorAll('.sheet')].map(s=>[...s.querySelectorAll(':scope>div>svg')].filter(v=>v.getBoundingClientRect().width>0).length)")
    chk("both engraved scores visible (1 svg each)", vis == [1, 1], vis)
    nw = pg.evaluate("Math.max(...[...document.querySelectorAll('.sheet>div>svg')].map(v=>v.getBoundingClientRect().width))")
    chk("score width usable", nw > vw * 0.7, round(nw))
    S = lambda e: pg.evaluate(f"window.__stand.{e}")
    nnotes = pg.evaluate("window.__DATA_SUMMARY.rows")
    shot("1-flute-selected")
    for inst in ("flute", "clarinet"):
        pg.locator(f"label:has(input[value={inst}])").click(); pg.wait_for_timeout(250)
        chk(f"select {inst}: state + radio + highlighted sheet", S("active") == inst and pg.is_checked(f"input[value={inst}]")
            and pg.evaluate(f"document.querySelector('.sheet[data-k={inst}]').classList.contains('active')"))
        # downloads: byte-identical to the originals
        for ext, sel in (("musicxml", "#dl-xml"), ("mid", "#dl-mid")):
            with pg.expect_download() as d: pg.click(sel)
            p = d.value.path(); got = open(p, "rb").read(); want = open(f"{parts}/{inst}.{ext}", "rb").read()
            chk(f"{inst}.{ext} download == original bytes", got == want, f"{sha(got)[:16]} {d.value.suggested_filename}")
        with pg.expect_download() as d: pg.click("#dl-wav")
        wv = open(d.value.path(), "rb").read(); chk(f"{inst} synth WAV downloads (RIFF)", wv[:4] == b"RIFF" and len(wv) > 100000, len(wv))
        # playback
        pg.click("#play"); pg.wait_for_timeout(2600)
        st = pg.evaluate("({s:window.__stand.ctxState,e:window.__stand.elapsed,p:window.__stand.maxPeak,pl:window.__stand.playing,n:window.__stand.scheduled,now:document.querySelectorAll('.sheet.active g.note.now').length,txt:document.querySelector('#nowtxt').innerText})")
        chk(f"{inst} play: AudioContext running, {nnotes} tones scheduled", st["s"] == "running" and st["pl"] and st["n"] == nnotes, st)
        chk(f"{inst} play: clock advanced ~2.5s", 1.8 < st["e"] < 3.6, round(st["e"], 2))
        chk(f"{inst} play: analyser heard signal (peak>0.02)", (st["p"] or 0) > .02, st["p"])
        chk(f"{inst} play: a note is highlighted", st["now"] >= 1, st["txt"])
        shot(f"2-{inst}-playing")
        pg.click("#play"); pg.wait_for_timeout(300); e1 = S("elapsed"); pg.wait_for_timeout(700)
        chk(f"{inst} pause: clock frozen", abs(S("elapsed") - e1) < .05 and S("paused"), [e1, S("elapsed")])
        pg.click("#play"); pg.wait_for_timeout(700)
        chk(f"{inst} resume: clock moves again", S("elapsed") > e1 + .2 and not S("paused"), S("elapsed"))
        pg.click("#stop"); pg.wait_for_timeout(200)
        chk(f"{inst} stop: reset", not S("playing") and S("elapsed") == 0)
        # play to completion from the last-but-two note
        fin0 = S("finished") or 0
        pg.locator(f".sheet[data-k={inst}] svg:visible g.note[data-i='{nnotes-2}'] .notehead").first.click(force=True)
        pg.wait_for_timeout(300); chk(f"{inst} click note {nnotes-1}: seeks & plays", S("playing") and S("idx") >= nnotes - 2, S("idx"))
        pg.wait_for_function("(window.__stand.finished||0)>" + str(fin0), timeout=12000)
        chk(f"{inst}: playback ran to the end and finished", not S("playing"))
    # cross-instrument pitches: clarinet shows written pitch label
    pg.locator("label:has(input[value=clarinet])").click(); pg.click("#play"); pg.wait_for_timeout(500)
    chk("clarinet readout shows sounding and written pitch", "written" in pg.inner_text("#nowtxt"), pg.inner_text("#nowtxt"))
    pg.click("#stop"); pg.locator("label:has(input[value=flute])").click(); pg.wait_for_timeout(300)
    shot("3-final")
    b.close(); return checks

if __name__ == "__main__":
    ap = argparse.ArgumentParser(); ap.add_argument("--site", required=True); ap.add_argument("--parts", required=True)
    ap.add_argument("--out", required=True); ap.add_argument("--tag", default="run1")
    a = ap.parse_args(); os.makedirs(a.out, exist_ok=True)
    allc = []
    with sync_playwright() as pw:
        for name, w, h in (("desktop", 1440, 900), ("mobile390", 390, 844)): allc += run(pw, a.site, a.parts, a.out, a.tag, name, w, h)
    ok = all(c["ok"] for c in allc)
    json.dump(dict(all_pass=ok, tag=a.tag, checks=allc), open(f"{a.out}/{a.tag}-browser.json", "w"), indent=1)
    for c in allc: print("PASS" if c["ok"] else "FAIL", c["name"], "|", c["detail"][:110])
    print("ALL PASS" if ok else "SOME FAILED", f"{sum(c['ok'] for c in allc)}/{len(allc)}"); raise SystemExit(0 if ok else 1)
