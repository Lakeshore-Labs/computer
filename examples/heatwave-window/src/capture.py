"""OUTSIDE Computer: drives system Chrome against the built page. Args: HTML OUTDIR"""

import sys, os, glob, shutil, json
from pathlib import Path
from playwright.sync_api import sync_playwright

html, out = sys.argv[1:3]
url = Path(html).resolve().as_uri()
Path(out).mkdir(parents=True, exist_ok=True)
observations = []
with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome", headless=True)
    errs = []
    for tag, w, h in (("desktop", 1280, 900), ("mobile-390", 390, 844)):
        pg = b.new_page(
            viewport={"width": w, "height": h},
            offline=True,
            device_scale_factor=2 if w < 500 else 1,
        )
        pg.on("pageerror", lambda e: errs.append(str(e)))
        pg.on("console", lambda m: m.type == "error" and errs.append(m.text))
        pg.goto(url)
        pg.wait_for_selector(".cell")
        pg.screenshot(path=f"{out}/{tag}-top.png")
        pg.screenshot(path=f"{out}/{tag}-full.png", full_page=True)
        sw = pg.evaluate("document.documentElement.scrollWidth")
        print(tag, "scrollWidth", sw, "viewport", w)
        assert sw == w
        counts = pg.locator("svg").evaluate_all(
            "(maps) => maps.filter(s=>s.querySelector('.cell')).map(s=>s.querySelectorAll('.cell').length)"
        )
        assert counts == [341, 341, 341], counts
        observations.append(
            {"viewport": w, "page_width": sw, "cells_in_three_maps": counts}
        )
        pg.close()
    # film
    ctx = b.new_context(
        offline=True,
        viewport={"width": 1280, "height": 800},
        record_video_dir=f"{out}/vid",
        record_video_size={"width": 1280, "height": 800},
    )
    pg = ctx.new_page()
    pg.goto(url)
    pg.wait_for_selector(".cell")
    pg.wait_for_timeout(900)
    for c in (1, 2):
        pg.locator("svg").nth(0).locator(".cell").nth(
            4 * 31 + 21 if c == 1 else 6 * 31 + 16
        ).click()
        pg.wait_for_timeout(900)
    pg.locator("svg").nth(1).focus()
    for k in ["ArrowRight"] * 3 + ["ArrowUp"] * 2:
        pg.keyboard.press(k)
        pg.wait_for_timeout(350)
    pg.click("#sh")
    pg.wait_for_timeout(900)
    pg.click("#sh")
    pg.wait_for_timeout(500)
    pg.click("#tv")
    pg.wait_for_timeout(1100)
    pg.mouse.wheel(0, 700)
    pg.wait_for_timeout(1000)
    ctx.close()
    b.close()
    if errs:
        raise RuntimeError(errs)
    (Path(out) / "browser.json").write_text(json.dumps(observations, indent=2) + "\n")
v = glob.glob(f"{out}/vid/*.webm")[0]
shutil.move(v, f"{out}/ui-film.webm")
os.rmdir(f"{out}/vid")
