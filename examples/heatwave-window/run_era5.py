#!/usr/bin/env python3
"""Reproducible driver for the ERA5 2 m temperature window (stdlib only).

  python3 run_era5.py --computer PATH/computer --python-wasi DIR --cache CACHE --out OUT [--live] [--validate]

Modes
  replay (default)  no network. Reads the upstream objects from --cache, checks every one against
                    src/pins.json (sha256), then runs src/analyze.py inside the public Computer CLI.
  --live            first downloads the pinned objects INSIDE Computer (stock curl, -allow-url limited to
                    the one store prefix) at their exact GCS generations into --cache, verifies them, then
                    runs the same analysis. Objects already valid in the cache are not fetched again.
  --validate        afterwards run src/validate.py OUTSIDE Computer with native zarr/numcodecs/numpy
                    (needs them importable; see src/validator-requirements.txt). Fails if they are missing.
  --probe-root      HEAD the live root .zattrs (outside Computer, informational) and report drift
                    from the pinned snapshot. Never used as an input.

Always: results are compared with expected/ (byte-identical is the pass condition) and a
checks.json is written to --out. Exit status 0 only if everything requested passed.
"""

import argparse, base64, hashlib, json, os, re, shutil, subprocess, sys, tempfile, urllib.request, urllib.error

HERE = os.path.dirname(os.path.abspath(__file__))
SRC, EXPECTED = os.path.join(HERE, "src"), os.path.join(HERE, "expected")
PINS = json.load(open(os.path.join(SRC, "pins.json")))
ORIGIN = PINS["origin"].rstrip("/") + "/"
checks = []


def check(name, ok, detail=""):
    checks.append(dict(name=name, ok=bool(ok), detail=detail))
    print(
        ("PASS " if ok else "FAIL ") + name + (" - " + detail if detail else ""),
        flush=True,
    )
    return ok


def fname(key):
    return key.replace("/", "_")


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for blk in iter(lambda: f.read(1 << 20), b""):
            h.update(blk)
    return h.hexdigest()


def header(path, name):
    m = re.search(
        r"^%s: (.*)$" % re.escape(name),
        open(path, encoding="latin1").read(),
        re.M | re.I,
    )
    return m.group(1).strip() if m else None


def verify_object(cache, o):
    """Returns '' if the cached object is exactly the pinned one, else a reason."""
    p = os.path.join(cache, fname(o["key"]))
    if not os.path.isfile(p):
        return "missing"
    if os.path.getsize(p) != o["bytes"] or sha256(p) != o["sha256"]:
        return "sha256/size differs from pin"
    if o["pinning"] == "immutable-generation" and os.path.isfile(p + ".headers"):
        h = p + ".headers"
        if header(h, "X-Goog-Generation") != o["generation"]:
            return "X-Goog-Generation differs from pin"
        m = re.search(r"md5=([^\s,]+)", header(h, "X-Goog-Hash") or "")
        if not m or base64.b64decode(m.group(1)).hex() != o["md5"]:
            return "X-Goog-Hash md5 differs from pin"
    return ""


def computer_cmd(a, mounts, extra, script):
    for host, _guest, _ in mounts:
        if ":" in host:
            sys.exit('error: host path with ":" cannot be given to -mount: ' + host)
    cmd = [a.computer]
    for host, guest, ro in mounts:
        cmd += ["-mount", "%s:%s%s" % (host, guest, ":ro" if ro else "")]
    cmd += extra + [
        "-python",
        os.path.join(a.python_wasi, "python.wasm"),
        "-python-lib",
        os.path.join(a.python_wasi, "lib"),
        "-c",
        script,
    ]
    return cmd


def live_fetch(a, cache, todo):
    with tempfile.TemporaryDirectory(prefix="era5 plan ") as plan:
        with open(os.path.join(plan, "plan.tsv"), "w") as f:
            for o in todo:
                f.write("%s\t%s\n" % (o["key"], o["generation"]))
        for o in todo:  # start clean so a partial earlier file is never mixed in
            for suf in ("", ".headers"):
                if os.path.exists(os.path.join(cache, fname(o["key"]) + suf)):
                    os.remove(os.path.join(cache, fname(o["key"]) + suf))
        cmd = computer_cmd(
            a,
            [(cache, "/src", False), (SRC, "/recipe", True), (plan, "/plan", True)],
            ["-allow-url", ORIGIN],
            "sh /recipe/fetch.sh",
        )
        print(
            "computer: fetching %d object(s) at pinned generations" % len(todo),
            flush=True,
        )
        r = subprocess.run(cmd)
        return r.returncode == 0


def main():
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument(
        "--computer",
        default=os.environ.get("COMPUTER") or shutil.which("computer"),
        help="path to the Computer CLI binary",
    )
    ap.add_argument(
        "--python-wasi",
        default=os.environ.get("PYTHON_WASI"),
        help="dir holding python.wasm and lib/ (release python-3.15.2-2)",
    )
    ap.add_argument(
        "--cache",
        required=True,
        help="upstream cache dir, kept OUTSIDE this repo; created if absent",
    )
    ap.add_argument("--out", required=True, help="output dir (must be empty or absent)")
    ap.add_argument("--live", action="store_true")
    ap.add_argument("--validate", action="store_true")
    ap.add_argument("--probe-root", action="store_true")
    a = ap.parse_args()
    for flag in ("computer", "python_wasi"):
        if not getattr(a, flag):
            sys.exit(
                "error: --%s is required (or set %s)"
                % (flag.replace("_", "-"), flag.upper())
            )
    a.computer, a.python_wasi = (
        os.path.abspath(a.computer),
        os.path.abspath(a.python_wasi),
    )
    cache, out = os.path.abspath(a.cache), os.path.abspath(a.out)
    if os.path.isdir(out) and os.listdir(out):
        sys.exit("error: --out is not empty: " + out)
    os.makedirs(cache, exist_ok=True)
    os.makedirs(out, exist_ok=True)
    if os.path.commonpath([cache, HERE]) == HERE:
        print(
            "warning: cache is inside the repo; do not commit upstream blobs",
            file=sys.stderr,
        )

    objs = PINS["objects"]
    # the mutable root .zattrs comes from the shipped snapshot, never from the network
    for o in objs:
        if o["pinning"] == "mutable-snapshot":
            shutil.copyfile(
                os.path.join(HERE, o["snapshot_file"]),
                os.path.join(cache, fname(o["key"])),
            )
    bad = {o["key"]: verify_object(cache, o) for o in objs}
    todo = [o for o in objs if bad[o["key"]]]
    if todo and a.live:
        ok = live_fetch(a, cache, todo)
        check("live fetch inside Computer exited 0 (generation-pinned reads)", ok)
        bad = {o["key"]: verify_object(cache, o) for o in objs}
        todo = [o for o in objs if bad[o["key"]]]
    for o in objs:
        check(
            "cache object %s" % o["key"],
            not bad[o["key"]],
            bad[o["key"]]
            or ("gen %s sha256 %s.." % (o["generation"], o["sha256"][:12])),
        )
    if todo:
        why = (
            "run again with --live"
            if not a.live
            else "a pinned generation may have been superseded or deleted upstream"
        )
        write_and_exit(out, "cache incomplete or not the pinned bytes (%s)" % why)
    check("mode", True, "live-then-replay" if a.live else "replay (no network)")

    if a.probe_root:
        try:
            req = urllib.request.Request(ORIGIN + ".zattrs", method="HEAD")
            with urllib.request.urlopen(req, timeout=30) as r:
                g = r.headers.get("X-Goog-Generation")
            pinned = [o for o in objs if o["key"] == ".zattrs"][0]["generation"]
            check(
                "probe-root (informational, outside Computer)",
                True,
                "live root generation %s %s pinned snapshot %s"
                % (
                    g,
                    "equals"
                    if g == pinned
                    else "DIFFERS from (store refreshed since snapshot; expected over time)",
                    pinned,
                ),
            )
        except (urllib.error.URLError, OSError) as e:
            check(
                "probe-root (informational, outside Computer)",
                True,
                "unreachable: %s" % e,
            )

    # --- analysis INSIDE Computer: no -allow-url, so no network
    cmd = computer_cmd(
        a,
        [(cache, "/src", True), (SRC, "/recipe", True), (out, "/out", False)],
        [],
        "python3 /recipe/analyze.py /src /out",
    )
    r = subprocess.run(cmd)
    check("analysis inside Computer exited 0", r.returncode == 0)
    for n in ("results.json", "window.csv", "index.html"):
        p = os.path.join(out, n)
        e = os.path.join(EXPECTED, n)
        same = os.path.isfile(p) and sha256(p) == sha256(e)
        check(
            "%s byte-identical to expected/" % n,
            same,
            sha256(p)[:16] if os.path.isfile(p) else "not produced",
        )

    if a.validate:
        try:
            r = subprocess.run(
                [sys.executable, "-I", "-c", "import zarr, numcodecs, numpy"],
                capture_output=True,
            )
            have = r.returncode == 0
        except OSError:
            have = False
        if not have:
            check(
                "native Zarr validation",
                False,
                "zarr/numcodecs/numpy not importable; create a venv from src/validator-requirements.txt and run with that python",
            )
        else:
            with tempfile.TemporaryDirectory(prefix="era5 mirror ") as mir:
                r = subprocess.run(
                    [
                        sys.executable,
                        "-I",
                        os.path.join(SRC, "validate.py"),
                        cache,
                        os.path.join(out, "results.json"),
                        os.path.join(out, "validation.json"),
                        mir,
                    ]
                )
            check(
                "native Zarr validation (outside Computer) all passed",
                r.returncode == 0,
                "see validation.json",
            )
    write_and_exit(out, None)


def write_and_exit(out, fatal):
    if fatal:
        check("fatal", False, fatal)
    allok = all(c["ok"] for c in checks)
    json.dump(
        dict(all_passed=allok, checks=checks),
        open(os.path.join(out, "checks.json"), "w"),
        indent=1,
    )
    print("RESULT:", "ALL PASSED" if allok else "FAILED", "(%d checks)" % len(checks))
    sys.exit(0 if allok else 1)


if __name__ == "__main__":
    main()
