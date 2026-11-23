#!/usr/bin/env python3
"""Real-browser verification + screen recording of the published shift desk.

usage: python3 -I capture.py --input DIR_WITH_observations.json --output DIR_WITH_shift.html+shift.sqlite --artifacts DIR
Needs Google Chrome (override with --chrome), ffmpeg, stdlib only. Chrome is driven over a CDP pipe;
the page is opened from file:// and every network request is logged (must all be file:/data:).
Writes into --artifacts: desktop-*.png, mobile-*.png, shift-desk.mp4, browser-report.json.
"""

import argparse, atexit, base64, json, os, re, select, sqlite3, subprocess, sys, tempfile, time
from contextlib import ExitStack
from pathlib import Path

ap = argparse.ArgumentParser()
ap.add_argument(
    "--input", required=True, type=Path
)  # kept for symmetry with audit.py; data are read from the published SQLite
ap.add_argument("--output", required=True, type=Path)
ap.add_argument("--artifacts", required=True, type=Path)
ap.add_argument(
    "--chrome", default="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
)
a = ap.parse_args()
art = a.artifacts
art.mkdir(parents=True, exist_ok=True)
page = (a.output / "shift.html").resolve()
# Register each resource as it is acquired so startup and capture failures unwind it too.
cleanup = ExitStack()
atexit.register(cleanup.close)
profile = cleanup.enter_context(tempfile.TemporaryDirectory(prefix="shiftdesk-chrome-"))
frames_dir = Path(cleanup.enter_context(tempfile.TemporaryDirectory(prefix="shiftdesk-frames-")))

# ---------- CDP over --remote-debugging-pipe (Chrome reads fd 3, writes fd 4; NUL-delimited JSON) ----------
c2b_r, c2b_w = os.pipe()
child_input = cleanup.enter_context(os.fdopen(c2b_r, "rb"))
cleanup.callback(os.close, c2b_w)
b2c_r, b2c_w = os.pipe()
child_output = cleanup.enter_context(os.fdopen(b2c_w, "wb"))
cleanup.callback(os.close, b2c_r)
chrome_args = [
    a.chrome,
    "--headless=new",
    "--remote-debugging-pipe",
    f"--user-data-dir={profile}",
    "--no-first-run",
    "--disable-gpu",
    "--hide-scrollbars",
    "--force-color-profile=srgb",
    "about:blank",
]
# a shell wrapper maps our pipe ends onto fd 3/4, which is where Chrome expects them
proc = subprocess.Popen(
    ["/bin/sh", "-c", f'exec "$@" 3<&{c2b_r} 4>&{b2c_w}', "sh", *chrome_args],
    pass_fds=(c2b_r, b2c_w),
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)
def stop_browser():
    if proc.poll() is None:
        proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


cleanup.callback(stop_browser)
child_input.close()
child_output.close()
buf = b""
mid = 0
events = []


def pump():
    global buf
    chunk = os.read(b2c_r, 1 << 20)
    if not chunk:
        raise RuntimeError("chrome closed pipe")
    buf += chunk


def msgs():
    global buf
    while b"\0" in buf:
        m, buf = buf.split(b"\0", 1)
        yield json.loads(m)


def call(method, params=None, session=None, timeout=30):
    global mid
    mid += 1
    me = mid
    d = {"id": me, "method": method, "params": params or {}}
    if session:
        d["sessionId"] = session
    os.write(c2b_w, json.dumps(d).encode() + b"\0")
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        for m in msgs():
            if m.get("id") == me:
                if "error" in m:
                    raise RuntimeError(f"{method}: {m['error']}")
                return m.get("result", {})
            elif "method" in m:
                events.append(m)
        if not select.select([b2c_r], [], [], max(0, end - time.monotonic()))[0]:
            break
        pump()
    raise TimeoutError(method)


def drain():
    while select.select([b2c_r], [], [], 0.3)[0]:
        pump()
        for m in msgs():
            if "method" in m:
                events.append(m)


browser_product = call("Browser.getVersion")["product"]
tid = call("Target.createTarget", {"url": "about:blank"})["targetId"]
S = call("Target.attachToTarget", {"targetId": tid, "flatten": True})["sessionId"]
cmd = lambda m, p=None: call(m, p, S)
for d in ("Page", "Runtime", "Network", "Log"):
    cmd(d + ".enable")


def ev(js):
    r = cmd(
        "Runtime.evaluate",
        {"expression": js, "returnByValue": True, "awaitPromise": True},
    )
    if "exceptionDetails" in r:
        raise RuntimeError(r["exceptionDetails"])
    return r["result"].get("value")


def viewport(w, h, mobile):
    cmd(
        "Emulation.setDeviceMetricsOverride",
        {
            "width": w,
            "height": h,
            "deviceScaleFactor": 2 if mobile else 1,
            "mobile": mobile,
        },
    )


def load():
    cmd("Page.navigate", {"url": page.as_uri()})
    for _ in range(100):
        time.sleep(0.1)
        try:
            if ev("document.readyState") == "complete" and ev(
                "!!document.getElementById('moment').textContent"
            ):
                return
        except RuntimeError:
            pass
    raise RuntimeError("page did not load")


def shot(path):
    w, h = ev(
        "[document.documentElement.scrollWidth, document.documentElement.scrollHeight]"
    )
    r = cmd(
        "Page.captureScreenshot",
        {
            "format": "png",
            "captureBeyondViewport": True,
            "clip": {"x": 0, "y": 0, "width": w, "height": h, "scale": 1},
        },
    )
    Path(path).write_bytes(base64.b64decode(r["data"]))


def setstate(i=None, g=None):
    js = "(()=>{"
    if g is not None:
        js += f"const s=document.getElementById('group');s.value='{g}';s.dispatchEvent(new Event('change'));"
    if i is not None:
        js += f"const t=document.getElementById('time');t.value={i};t.dispatchEvent(new Event('input'));"
    ev(js + "})()")


# ---------- expected values from SQLite (independent of the page's JS) ----------
db = sqlite3.connect(f"file:{a.output / 'shift.sqlite'}?mode=ro", uri=True)
cleanup.callback(db.close)
times = [
    r[0]
    for r in db.execute(
        "select capture from tide union select capture from bike union select capture from hosted union select capture from client order by 1"
    )
]
latest = lambda table, t: db.execute(
    f"select max(capture) from {table} where capture<=?", (t,)
).fetchone()[0]
fmt_s = lambda x: str(float(x)).removesuffix(".0")
NM = {"astro": "Astro", "mslearn": "Microsoft Learn", "svelte": "Svelte"}


def expected(t, g):
    e = {}
    tc = latest("tide", t)
    e["tides"] = (
        sorted(
            (c, f"{m:.3f} m")
            for c, m in db.execute(
                "select city, metres from tide where capture=?", (tc,)
            )
        )
        if tc
        else []
    )
    bc = latest("bike", t)
    if bc:
        st, bk = db.execute(
            "select stations,bikes from bike where capture=?", (bc,)
        ).fetchone()
        e["bike_total"] = f"{bk} bikes reported across {st} stations."
        stops = db.execute(
            "select name,bikes,renting from stop where capture=?", (bc,)
        ).fetchall()
        e["stops"] = sorted((n, str(b)) for n, b, _ in stops)
        e["eligible"] = sorted(n for n, b, r in stops if r and b >= g)
    else:
        e["bike_total"] = "No Oslo feed collected yet."
        e["stops"] = []
        e["eligible"] = None
    hc = latest("hosted", t)
    e["hosted"] = (
        sorted(
            (NM[n], ("Passed · " + fmt_s(s) + " s") if p else "Check missed")
            for n, p, s in db.execute(
                "select name,passed,seconds from hosted where capture=?", (hc,)
            )
        )
        if hc
        else []
    )
    cc = latest("client", t)
    if cc:
        age, dig = db.execute(
            "select age_seconds,digest from client where capture=?", (cc,)
        ).fetchone()
        e["age"] = f"{age // 3600}h {age % 3600 // 60}m"
        e["client_n"] = db.execute(
            "select count(*) from client where capture<=?", (t,)
        ).fetchone()[0]
        e["digest12"] = dig[:12]
    else:
        e["age"] = ""
        e["client_n"] = 0
    return e


READ = """(()=>{const q=s=>document.querySelector(s),rows=s=>[...document.querySelectorAll(s+' .reading')].map(r=>[r.children[0].textContent,r.children[1].textContent]);
return {moment:q('#moment').textContent,position:q('#position').textContent,tides:rows('#tides'),bike_total:q('#bike-total').textContent,stops:rows('#stops'),eligible:q('#eligible').textContent,
hosted:rows('#hosted'),hosted_time:q('#docs-time').textContent,age:q('#age').textContent,client:q('#client').textContent,client_time:q('#client-time').textContent,group:q('#group').value,
prev:q('#prev').disabled,next:q('#next').disabled,overflow:document.documentElement.scrollWidth>document.documentElement.clientWidth}})()"""


def verify(i, g):
    t = times[i]
    e = expected(t, g)
    d = ev(READ)
    bad = []
    if d["moment"] != t.replace("T", " ")[:19] + " UTC":
        bad.append(("moment", d["moment"], t))
    if d["position"] != f"{i + 1} / {len(times)}":
        bad.append(("position", d["position"]))
    if d["group"] != str(g):
        bad.append(("group", d["group"]))
    if sorted(map(tuple, d["tides"])) != e["tides"]:
        bad.append(("tides", d["tides"], e["tides"]))
    if d["bike_total"] != e["bike_total"]:
        bad.append(("bike_total", d["bike_total"], e["bike_total"]))
    if sorted(map(tuple, d["stops"])) != e["stops"]:
        bad.append(("stops", d["stops"], e["stops"]))
    if e["eligible"] is not None:
        m = re.fullmatch(rf"Enough for {g} at (.*)\.", d["eligible"])
        got = (
            None
            if not m
            else (
                []
                if m.group(1) == "none of these three stations"
                else sorted(m.group(1).split(" and "))
            )
        )
        if got != e["eligible"]:
            bad.append(("eligible", d["eligible"], e["eligible"]))
    elif d["eligible"] != "":
        bad.append(("eligible", d["eligible"]))
    if sorted(map(tuple, d["hosted"])) != e["hosted"]:
        bad.append(("hosted", d["hosted"], e["hosted"]))
    if e["age"] != d["age"]:
        bad.append(("age", d["age"], e["age"]))
    if e["client_n"]:
        if (
            not d["client"].startswith(f"{e['client_n']} tide snapshots")
            or e["digest12"] not in d["client_time"]
        ):
            bad.append(("client", d["client"], d["client_time"]))
    elif d["client"] != "The persistent client had not started.":
        bad.append(("client", d["client"]))
    if d["prev"] != (i == 0) or d["next"] != (i == len(times) - 1):
        bad.append(("buttons", d["prev"], d["next"]))
    return d, bad


last = len(times) - 1
report = {
    "page": page.name,
    "chrome": browser_product,
    "times": len(times),
    "scenes": {},
    "failures": [],
}

# ---------- 1. exhaustive DOM-vs-SQLite pass (not recorded) ----------
viewport(1280, 900, False)
load()
bad_all = []
n = 0
for g in (5, 10, 13, 20):
    for i in range(len(times)):
        setstate(i, g)
        _, bad = verify(i, g)
        n += 1
        if bad:
            bad_all.append({"index": i, "group": g, "bad": bad})
report["exhaustive"] = {
    "states_checked": n,
    "mismatches": len(bad_all),
    "details": bad_all[:10],
}
report["failures"] += [
    f"exhaustive mismatch at {b['index']}/{b['group']}" for b in bad_all
]

setstate(0, 10)
d0 = ev(READ)
first_ok = bool(
    d0["bike_total"] == "No Oslo feed collected yet."
    and d0["hosted"] == []
    and d0["hosted_time"] == "No hosted round collected yet."
    and d0["client"] == "The persistent client had not started."
    and d0["tides"]
)
report["scenes"]["earliest_state"] = {
    "ok": first_ok,
    "moment": d0["moment"],
    "bike_total": d0["bike_total"],
    "hosted_time": d0["hosted_time"],
    "client": d0["client"],
}
if not first_ok:
    report["failures"].append("earliest state shows bike/hosted/client data")
report["scenes"]["latest_group_10_vs_13"] = {
    "10": expected(times[last], 10)["eligible"],
    "13": expected(times[last], 13)["eligible"],
}
report["scenes"]["times_where_10_and_13_differ"] = [
    times[i]
    for i in range(len(times))
    if latest("bike", times[i])
    and expected(times[i], 10)["eligible"] != expected(times[i], 13)["eligible"]
]
if expected(times[last], 10)["eligible"] == expected(times[last], 13)["eligible"]:
    report["scenes"]["note"] = (
        "latest eligibility is identical for 10 and 13 in this sample"
    )


# ---------- 2. clean stills ----------
def stills(prefix, w, h, mobile):
    viewport(w, h, mobile)
    load()
    for name, idx in (("earliest", 0), ("latest", last)):
        for g in (10, 13):
            setstate(idx, g)
            time.sleep(0.3)
            d, bad = verify(idx, g)
            if bad:
                report["failures"].append(f"{prefix} {name} g{g}: {bad}")
            if d["overflow"]:
                report["failures"].append(f"{prefix} {name} g{g}: horizontal overflow")
            if name == "earliest" and g == 13:
                continue  # identical to g10 (no bike feed yet)
            shot(art / f"{prefix}-{name}-group{g}.png")
            report["scenes"][f"{prefix}-{name}-group{g}"] = {
                "moment": d["moment"],
                "bike_total": d["bike_total"],
                "eligible": d["eligible"],
                "overflow": d["overflow"],
            }


stills("desktop", 1280, 900, False)
stills("mobile", 390, 844, True)

# ---------- 3. recorded interaction (real mouse drag on the slider) ----------
viewport(1280, 720, False)
load()
setstate(last, 10)
frames = []
t_start = time.time()


def grab():
    f = frames_dir / f"f{len(frames):05d}.jpg"
    f.write_bytes(
        base64.b64decode(
            cmd("Page.captureScreenshot", {"format": "jpeg", "quality": 88})["data"]
        )
    )
    frames.append((f, time.time()))


def hold(sec):
    end = time.time() + sec
    while time.time() < end:
        grab()


def mouse(typ, x, y):
    cmd(
        "Input.dispatchMouseEvent",
        {
            "type": typ,
            "x": x,
            "y": y,
            "button": "left",
            "buttons": 0 if typ == "mouseReleased" else 1,
            "clickCount": 1,
        },
    )


def sx(frac):
    l, y, w = ev(
        "(()=>{const r=document.getElementById('time').getBoundingClientRect();return [r.left,r.top+r.height/2,r.width]})()"
    )
    return l + 8 + frac * (w - 16), y


rec = []


def snap(label, g):
    i = int(ev("document.getElementById('time').value"))
    d, bad = verify(i, g)
    rec.append(
        {
            "label": label,
            "index": i,
            "time": times[i],
            "group": g,
            "ok": not bad,
            "eligible": d["eligible"],
        }
    )
    if bad:
        report["failures"].append(f"recording {label}: {bad}")


hold(1.5)
snap("latest, group 10", 10)
ev(
    "(()=>{const s=document.getElementById('group');s.value='13';s.dispatchEvent(new Event('change'))})()"
)
hold(2.0)
snap("latest, group 13", 13)
ev(
    "(()=>{const s=document.getElementById('group');s.value='10';s.dispatchEvent(new Event('change'))})()"
)
hold(1.0)
snap("latest, group 10 again", 10)
x, y = sx(1.0)
mouse("mouseMoved", x, y)
mouse("mousePressed", x, y)
for k in range(1, 31):
    x, y = sx(1 - k / 30)
    mouse("mouseMoved", x, y)
    grab()
mouse("mouseReleased", x, y)
hold(1.5)
snap("earliest after drag", 10)
for _ in range(3):
    ev("document.getElementById('next').click()")
    hold(0.6)
snap("earliest + 3 steps", 10)
t_end = time.time()
lines = []
for k, (f, ts) in enumerate(frames):
    dur = (frames[k + 1][1] - ts) if k + 1 < len(frames) else 0.2
    lines += [f"file '{f}'", f"duration {dur:.4f}"]
lines.append(f"file '{frames[-1][0]}'")
(frames_dir / "list.txt").write_text("\n".join(lines) + "\n")
mp4 = art / "shift-desk.mp4"
subprocess.run(
    [
        "ffmpeg",
        "-y",
        "-loglevel",
        "error",
        "-f",
        "concat",
        "-safe",
        "0",
        "-i",
        str(frames_dir / "list.txt"),
        "-vf",
        "fps=20,scale=1280:720,format=yuv420p",
        "-c:v",
        "libx264",
        "-crf",
        "20",
        "-movflags",
        "+faststart",
        str(mp4),
    ],
    check=True,
)
report["recording"] = {
    "file": mp4.name,
    "frames": len(frames),
    "wall_seconds": round(t_end - t_start, 2),
    "checks": rec,
}

# ---------- network / console ----------
drain()
urls = sorted(
    {
        e["params"]["request"]["url"]
        for e in events
        if e.get("method") == "Network.requestWillBeSent"
    }
)
report["network_urls"] = [page.name if u == page.as_uri() else u for u in urls]
report["external_requests"] = [
    u for u in urls if not u.startswith(("file:", "data:", "about:", "blob:"))
]
if report["external_requests"]:
    report["failures"].append("external requests made")
report["console_problems"] = [
    e["params"]
    for e in events
    if e.get("method") == "Runtime.exceptionThrown"
    or (
        e.get("method") == "Log.entryAdded"
        and e["params"]["entry"]["level"] in ("error", "warning")
    )
    or (
        e.get("method") == "Runtime.consoleAPICalled"
        and e["params"]["type"] in ("error", "warning")
    )
]
if report["console_problems"]:
    report["failures"].append(f"{len(report['console_problems'])} console problems")
report["ok"] = not report["failures"]
(art / "browser-report.json").write_text(
    json.dumps(report, indent=2, ensure_ascii=False) + "\n"
)
cleanup.close()
print(
    json.dumps(
        {k: report[k] for k in ("ok", "failures", "exhaustive", "external_requests")},
        indent=1,
    )
)
print(
    "recording",
    report["recording"]["wall_seconds"],
    "s,",
    report["recording"]["frames"],
    "frames",
)
sys.exit(0 if report["ok"] else 1)
