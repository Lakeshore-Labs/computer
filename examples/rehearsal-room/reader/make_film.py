#!/usr/bin/env python3
"""Record a ~17 s film of the real reader in Chromium: flute plays, selector switches to
clarinet, clarinet plays. Headless Chromium records video only, so the soundtrack is the
page's own synthesized WAVs (audio/<part>-synth.wav) muxed at the wall-clock moments the
play button was pressed (sync is approximate, +/-~0.3 s). Needs ffmpeg on PATH.

usage: make_film.py --site site --out film/run1-desktop.mp4 [--width 1280 --height 800]
"""
import argparse, os, subprocess, tempfile, time, shutil
from playwright.sync_api import sync_playwright

ap = argparse.ArgumentParser(); ap.add_argument("--site", required=True); ap.add_argument("--out", required=True)
ap.add_argument("--width", type=int, default=1280); ap.add_argument("--height", type=int, default=800)
ap.add_argument("--zoom", type=float, default=1.0, help="CSS zoom so the whole stand fits the frame")
a = ap.parse_args()
os.makedirs(os.path.dirname(os.path.abspath(a.out)), exist_ok=True)
tmp = tempfile.mkdtemp(prefix="film-", dir=os.path.dirname(os.path.abspath(a.out)))
SEG = 6.5  # seconds of each instrument shown/heard
with sync_playwright() as pw:
    b = pw.chromium.launch(args=["--autoplay-policy=no-user-gesture-required"])
    t_ctx = time.time()
    c = b.new_context(viewport=dict(width=a.width, height=a.height), record_video_dir=tmp,
                      record_video_size=dict(width=a.width, height=a.height))
    pg = c.new_page(); pg.goto("file://" + os.path.abspath(f"{a.site}/index.html")); pg.wait_for_timeout(500)
    if a.zoom != 1: pg.evaluate(f"document.body.style.zoom={a.zoom}")
    pg.wait_for_timeout(1000)
    marks = {}
    def focus(sel): pg.locator(sel).scroll_into_view_if_needed()
    pg.mouse.move(a.width / 2, 200)
    for inst in ("flute", "clarinet"):
        pg.locator(f"label:has(input[value={inst}])").click(); pg.wait_for_timeout(900)
        pg.evaluate("document.querySelector('#play').scrollIntoView({block:'center'})") if a.width < 500 else None
        marks[inst] = time.time() - t_ctx
        pg.click("#play"); pg.evaluate("window.scrollTo(0,0)"); pg.wait_for_timeout(int(SEG * 1000))
        pg.click("#stop"); pg.wait_for_timeout(500)
    pg.evaluate("window.scrollTo(0,0)"); pg.wait_for_timeout(700)
    vid = pg.video; c.close(); b.close(); webm = vid.path()
parts = list(marks)
inputs, filt = ["-i", webm], []
for i, k in enumerate(parts):
    inputs += ["-i", f"{a.site}/audio/{k}-synth.wav"]
    ms = int((marks[k] + 0.35) * 1000)  # page start-up before video t=0 is unknown; ~0.35 s offset measured empirically
    filt.append(f"[{i+1}:a]atrim=0:{SEG},adelay={ms}|{ms}[a{i}]")
mix = "".join(f"[a{i}]" for i in range(len(parts))) + f"amix=inputs={len(parts)}:normalize=0[a]"
subprocess.run(["ffmpeg", "-y", "-loglevel", "error", *inputs, "-filter_complex", ";".join(filt + [mix]), "-map", "0:v", "-map", "[a]",
                "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "20", "-c:a", "aac", "-b:a", "128k", "-shortest", a.out], check=True)
shutil.rmtree(tmp)
d = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", a.out], capture_output=True, text=True).stdout.strip()
print(f"film {a.out} duration {float(d):.1f}s play marks {marks}")
