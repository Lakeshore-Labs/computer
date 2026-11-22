#!/usr/bin/env python3 -I
"""Storage interoperability lab: deliver the Last Boat Home flute MIDI + MusicXML to account-free rclone backends.

Run:  python3 -I run-lab.py --computer PATH --source DIR --lab EMPTY_DIR --out EVIDENCE_DIR
Everything runs on loopback with a NEW temp rclone config, synthetic roots and generated credentials.
It never reads ~/.config/rclone or any real remote."""

import argparse, base64, hashlib, json, os, platform, re, secrets, shutil, socket, subprocess, sys, tempfile, time, urllib.request
from pathlib import Path
from shlex import quote

HERE = Path(__file__).resolve().parent
SRC_SHA = {
    "flute.mid": "9098ba67dc8393ab21c0c4f77dae74cc591ba170eb6ee3343e7ecdbeea015e47",
    "flute.musicxml": "94791b09d17580631fb9e427aede257bd32ab9ca5f17edb2584ee5c5c7bfb82a",
}
FILES = ["flute.mid", "flute.musicxml"]
sha = lambda b: hashlib.sha256(b).hexdigest()


def sh(cmd, env=None, timeout=120, inp=None):
    p = subprocess.run(cmd, env=env, capture_output=True, timeout=timeout, input=inp)
    return p.returncode, p.stdout, p.stderr


class Scrub:
    def __init__(self):
        self.pairs = []

    def add(self, a, b):
        if a:
            self.pairs.append((str(a), b))

    def __call__(self, s):
        if isinstance(s, bytes):
            s = s.decode("utf-8", "replace")
        for a, b in sorted(self.pairs, key=lambda p: -len(p[0])):
            s = s.replace(a, b)
        return s


def stdio_mcp(argv, env, requests, wait=2.5):
    """Speak raw MCP JSON-RPC over stdio. Returns (replies by id, stdout_text, stderr_text, exit_code)."""
    p = subprocess.Popen(
        argv,
        env=env,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    msgs = [
        {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "lab-probe", "version": "0"},
            },
        },
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
    ] + requests
    try:
        for m in msgs:
            p.stdin.write((json.dumps(m) + "\n").encode())
            p.stdin.flush()
    except BrokenPipeError:
        pass
    time.sleep(wait)
    try:
        p.stdin.close()
    except Exception:
        pass
    try:
        out, err = p.communicate(timeout=10)
    except subprocess.TimeoutExpired:
        p.kill()
        out, err = p.communicate()
    replies = {}
    for line in out.decode("utf-8", "replace").splitlines():
        try:
            d = json.loads(line)
        except ValueError:
            continue
        if "id" in d:
            replies[d["id"]] = d
    return (
        replies,
        out.decode("utf-8", "replace"),
        err.decode("utf-8", "replace"),
        p.returncode,
    )


def backends(L):
    R = f"{L}/roots"
    mid, xml = FILES
    plain = lambda d: {mid: f"{R}/{d}/{mid}", xml: f"{R}/{d}/{xml}"}
    return [
        dict(
            id="local",
            rtype="local",
            remote="lab-local",
            dst=f"lab-local:{R}/local-dest",
            paths={mid: mid, xml: xml},
            roots=["local-dest"],
            plain=plain("local-dest"),
            what="Ordinary directory on this machine, addressed through a named local remote.",
        ),
        dict(
            id="alias",
            rtype="alias",
            remote="lab-alias",
            dst="lab-alias:",
            paths={mid: mid, xml: xml},
            roots=["alias-real"],
            plain=plain("alias-real"),
            what="A second name for a directory; writing to the alias writes the target unchanged.",
        ),
        dict(
            id="combine",
            rtype="combine",
            remote="lab-combine",
            dst="lab-combine:",
            paths={mid: f"part-midi/{mid}", xml: f"part-xml/{xml}"},
            roots=["combine-a", "combine-b"],
            plain={mid: f"{R}/combine-a/{mid}", xml: f"{R}/combine-b/{xml}"},
            check=False,
            what="Two directories presented as one tree: the MIDI is routed to part-midi/ and the MusicXML to part-xml/, each landing in a different physical upstream.",
        ),
        dict(
            id="union",
            rtype="union",
            remote="lab-union",
            dst="lab-union:",
            paths={mid: mid, xml: xml},
            roots=["union-seeded", "union-b"],
            plain=plain("union-b"),
            what="Two upstreams merged. The first is no-create (:nc) and pre-seeded with one synthetic note, so both score files must be created in the second.",
        ),
        dict(
            id="hasher",
            rtype="hasher",
            remote="lab-hasher",
            dst="lab-hasher:",
            paths={mid: mid, xml: xml},
            roots=["hasher-store"],
            plain=plain("hasher-store"),
            what="A pass-through wrapper that keeps md5/sha1/sha256 sums in a database (stored in this lab's own cache dir); files on disk stay plain.",
        ),
        dict(
            id="compress",
            rtype="compress",
            remote="lab-compress",
            dst="lab-compress:",
            paths={mid: mid, xml: xml},
            roots=["compress-store"],
            plain=None,
            what="gzip-compressing wrapper: the stored objects are not the original bytes, but reading through the remote restores them.",
        ),
        dict(
            id="chunker",
            rtype="chunker",
            remote="lab-chunker",
            dst="lab-chunker:",
            paths={mid: mid, xml: xml},
            roots=["chunker-store"],
            plain=None,
            chunks=True,
            what="Splits files into 1 KiB chunks. The 3,271-byte MusicXML becomes 4 chunk files plus a small metadata file; the 242-byte MIDI stays whole.",
        ),
        dict(
            id="crypt",
            rtype="crypt",
            remote="lab-crypt",
            dst="lab-crypt:",
            paths={mid: mid, xml: xml},
            roots=["crypt-store"],
            plain=None,
            crypt=True,
            what="Encrypts names and contents with generated throwaway passwords. The physical store holds only ciphertext under scrambled names.",
        ),
        dict(
            id="memory",
            rtype="memory",
            remote="lab-memory",
            dst="lab-memory:scores",
            paths={mid: mid, xml: xml},
            roots=[],
            plain=None,
            memory=True,
            what="In-memory object store inside the running rclone rcd process, addressed as bucket scores/ (the memory backend is bucket-based). Nothing is written to disk and it vanishes when rcd stops.",
        ),
    ]


def join(dst, path):
    return dst + path if dst.endswith(":") else dst + "/" + path


def parse_sections(text):
    out, cur, buf = {}, None, []
    for line in text.splitlines():
        m = re.match(r"^@@ (\S+)$", line)
        if m:
            if cur:
                out[cur] = "\n".join(buf).strip()
            cur, buf = m.group(1), []
        elif cur:
            buf.append(line)
    if cur:
        out[cur] = "\n".join(buf).strip()
    for k, v in out.items():
        try:
            out[k] = json.loads(v)
        except ValueError:
            out[k] = {"text": v}
    return out


def physical_inventory(L, b):
    inv = []
    for r in b["roots"]:
        base = Path(L) / "roots" / r
        for f in sorted(base.rglob("*")):
            if f.is_file():
                data = f.read_bytes()
                inv.append(
                    {
                        "root": r,
                        "path": str(f.relative_to(base)),
                        "bytes": len(data),
                        "sha256": sha(data),
                        "first8_hex": data[:8].hex(),
                    }
                )
    return inv


def native_sha(L, b, inv):
    """Independent of rclone: hash bytes read straight from the physical files (plain or chunk concatenation)."""
    res = {}
    for f in FILES:
        if b.get("plain"):
            p = Path(b["plain"][f])
            res[f] = sha(p.read_bytes()) if p.exists() else None
        elif b.get("chunks"):
            base = Path(L) / "roots" / b["roots"][0]
            parts = sorted(base.glob(f + ".rclone_chunk.*"))
            if parts:
                res[f] = sha(b"".join(p.read_bytes() for p in parts))
            elif (base / f).exists() and (base / f).stat().st_size == len(
                (Path(L) / "source" / f).read_bytes()
            ):
                res[f] = sha((base / f).read_bytes())
            else:
                res[f] = None
        elif b["id"] == "compress":
            import gzip

            base = Path(L) / "roots" / b["roots"][0]
            gz = sorted(base.glob(f + ".*.gz"))
            res[f] = sha(gzip.decompress(gz[0].read_bytes())) if gz else None
        else:
            res[f] = None
    return res


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--computer", required=True)
    ap.add_argument("--source", required=True)
    ap.add_argument("--lab", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument(
        "--check-versions",
        action="store_true",
        help="also try three historical unpinned package combinations",
    )
    a = ap.parse_args()
    lab = Path(a.lab).resolve()
    out = Path(a.out).resolve()
    assert not lab.exists() or not any(lab.iterdir()), "--lab must be new/empty"
    lab.mkdir(parents=True, exist_ok=True)
    out.mkdir(parents=True, exist_ok=False)
    scrub = Scrub()
    scrub.add(lab, "$LAB")
    scrub.add(a.source, "$SOURCE")
    scrub.add(Path.home(), "~")
    scrub.add(os.environ.get("USER"), "user")
    # --- source: only the two original artifacts, verified against the pinned hashes
    src = lab / "source"
    src.mkdir()
    for f in FILES:
        data = (Path(a.source) / f).read_bytes()
        assert sha(data) == SRC_SHA[f], f"{f}: not the pinned original"
        (src / f).write_bytes(data)
    # --- pinned MCP install from the checked lockfile
    mcp = lab / "mcp"
    mcp.mkdir()
    for n in ("package.json", "package-lock.json"):
        shutil.copy(HERE / "mcp-pin" / n, mcp / n)
    r = subprocess.run(
        [
            "npm",
            "ci",
            "--ignore-scripts",
            "--no-audit",
            "--no-fund",
            "--prefer-offline",
        ],
        cwd=mcp,
        capture_output=True,
        timeout=300,
    )
    assert r.returncode == 0, r.stderr.decode()[-500:]
    mcp_js = mcp / "node_modules/rclone-mcp/dist/index.js"
    pkgv = lambda n: json.loads(
        (mcp / "node_modules" / n / "package.json").read_text()
    )["version"]
    # --- NEW temp rclone config + roots
    (lab / "cache").mkdir()
    (lab / "tmp").mkdir()
    isolated = {
        "HOME": str(lab),
        "PATH": os.environ["PATH"],
        "TMPDIR": str(lab / "tmp"),
    }
    r = subprocess.run(
        ["bash", str(HERE / "setup-lab.sh"), str(lab), str(src)],
        capture_output=True,
        text=True,
        env=isolated,
    )
    assert r.returncode == 0, r.stderr
    conf = lab / "rclone.conf"
    renv = {
        **isolated,
        "RCLONE_CONFIG": str(conf),
        "RCLONE_CACHE_DIR": str(lab / "cache"),
    }
    # --- loopback rcd with generated credentials
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    user, pw = "lab" + secrets.token_hex(3), secrets.token_hex(16)
    for x in (user, pw):
        scrub.add(x, "<generated>")
    url = f"http://127.0.0.1:{port}"
    rcd = subprocess.Popen(
        [
            "rclone",
            "rcd",
            "--rc-addr",
            f"127.0.0.1:{port}",
            "--rc-user",
            user,
            "--rc-pass",
            pw,
            "--rc-serve",
            "--max-delete",
            "0",
            "--log-file",
            str(lab / "rcd.log"),
            "-v",
        ],
        env=renv,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    auth = "Basic " + base64.b64encode(f"{user}:{pw}".encode()).decode()

    def rc(path, body=None):
        req = urllib.request.Request(
            url + path,
            data=json.dumps(body or {}).encode(),
            headers={"Authorization": auth, "Content-Type": "application/json"},
            method="POST",
        )
        return json.loads(urllib.request.urlopen(req, timeout=20).read())

    for _ in range(50):
        try:
            ver = rc("/core/version")
            break
        except Exception:
            time.sleep(0.2)
    else:
        rcd.terminate()
        rcd.wait(timeout=10)
        raise RuntimeError("The local rclone server did not start")
    mcp_env = {**isolated, "RCLONE_URL": url, "RCLONE_USER": user, "RCLONE_PASS": pw}
    ev = {
        "generated_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "rclone": {
            "version": ver["version"],
            "os_arch": ver["osArch"],
            "go": ver["goVersion"],
            "config": "new empty temp rclone.conf generated by setup-lab.sh (never ~/.config/rclone)",
            "rc": "loopback 127.0.0.1, random port, generated user/password discarded after the run, --max-delete 0",
        },
        "rclone_mcp": {
            "package": "rclone-mcp",
            "version": pkgv("rclone-mcp"),
            "self_reported_server_version": None,
            "pinned_deps": {
                "rclone-sdk": pkgv("rclone-sdk"),
                "rclone-openapi": pkgv("rclone-openapi"),
            },
            "repository": "https://github.com/rclone-ui/rclone-mcp",
            "license": "MIT",
        },
        "node": subprocess.run(
            ["node", "-v"], capture_output=True, text=True
        ).stdout.strip(),
        "platform": platform.platform(),
        "probes": [],
        "backends": [],
    }
    try:
        # --- version probes: honest record of which published rclone-mcp releases accept this rclone
        if a.check_versions:
            for vers, overrides in (("2.1.0", None), ("2.0.0", None), ("1.0.0", None)):
                d = Path(tempfile.mkdtemp(prefix="lab-probe-"))
                try:
                    (d / "package.json").write_text(
                        json.dumps(
                            {
                                "name": "probe",
                                "version": "0.0.0",
                                "private": True,
                                "dependencies": {"rclone-mcp": vers},
                            }
                        )
                    )
                    r = subprocess.run(
                        [
                            "npm",
                            "install",
                            "--ignore-scripts",
                            "--no-audit",
                            "--no-fund",
                            "--prefer-offline",
                        ],
                        cwd=d,
                        capture_output=True,
                        timeout=300,
                    )
                    if r.returncode:
                        ev["probes"].append({"rclone_mcp": vers, "install": "failed"})
                        continue
                    rep, so, se, code = stdio_mcp(
                        [
                            "node",
                            str(d / "node_modules/rclone-mcp/dist/index.js"),
                            "stdio",
                        ],
                        {**mcp_env, "RCLONE_TOOLSETS": "operations"},
                        [
                            {
                                "jsonrpc": "2.0",
                                "id": 3,
                                "method": "tools/call",
                                "params": {
                                    "name": "operations_list",
                                    "arguments": {"fs": "lab-memory:", "remote": ""},
                                },
                            }
                        ],
                        wait=3,
                    )
                    call = (
                        rep.get(3, {})
                        .get("result", {})
                        .get("content", [{}])[0]
                        .get("text")
                    )
                    ev["probes"].append(
                        {
                            "rclone_mcp": vers,
                            "dependency_ranges": "as published (unpinned, newest allowed)",
                            "rclone_sdk": json.loads(
                                (d / "node_modules/rclone-sdk/package.json").read_text()
                            )["version"],
                            "against_rclone": ver["version"],
                            "exit_code": code,
                            "stderr": scrub(se).strip()[:400],
                            "operations_list_result": scrub(call) if call else None,
                            "outcome": "refused rclone version at startup"
                            if "needs rclone" in se
                            else (
                                "tool call failed"
                                if call and call.startswith("Error")
                                else "worked"
                            ),
                        }
                    )
                finally:
                    shutil.rmtree(d, ignore_errors=True)
        # --- official tool names exactly as the pinned server publishes them
        reps, _, se, _ = stdio_mcp(
            ["node", str(mcp_js), "stdio"],
            {**mcp_env, "RCLONE_TOOLSETS": "operations,sync"},
            [{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}],
        )
        ev["rclone_mcp"]["self_reported_server_version"] = reps[1]["result"][
            "serverInfo"
        ]["version"]
        copy_tools = sorted(t["name"] for t in reps[2]["result"]["tools"])
        reps, _, _, _ = stdio_mcp(
            ["node", str(mcp_js), "stdio", "--read-only"],
            {**mcp_env, "RCLONE_TOOLSETS": "operations"},
            [{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}],
        )
        read_tools = sorted(t["name"] for t in reps[2]["result"]["tools"])
        ev["tool_surface"] = {
            "rclone-copy": {
                "launch": "rclone-mcp stdio, RCLONE_TOOLSETS=operations,sync",
                "published_tools": copy_tools,
                "used": ["operations_copyfile", "sync_copy"],
                "enforcement": "toolset only; delete/move/sync tools are still published. Restriction to copy is by the fixed call plan and the recorded ledger, not by the server.",
            },
            "rclone-read": {
                "launch": "rclone-mcp stdio --read-only, RCLONE_TOOLSETS=operations",
                "published_tools": read_tools,
                "used": [
                    "operations_list",
                    "operations_stat",
                    "operations_hashsumfile",
                    "operations_size",
                    "operations_check",
                ],
                "enforcement": "server-enforced: only tools rclone-mcp marks read-only are registered",
            },
        }
        # --- Computer config: two stock upstream MCP servers over stdio
        cfg = {
            "mcpServers": {
                n: {
                    "command": "node",
                    "args": [str(mcp_js), "stdio"] + extra,
                    "env": {
                        "RCLONE_URL": "${RCLONE_URL}",
                        "RCLONE_USER": "${RCLONE_USER}",
                        "RCLONE_PASS": "${RCLONE_PASS}",
                        "RCLONE_TOOLSETS": ts,
                    },
                }
                for n, extra, ts in (
                    ("rclone-copy", [], "operations,sync"),
                    ("rclone-read", ["--read-only"], "operations"),
                )
            }
        }
        (lab / "computer.json").write_text(json.dumps(cfg, indent=2))
        comp = a.computer

        def computer(script, label):
            rcode, so, se = sh(
                [comp, "-config", str(lab / "computer.json"), "-c", script],
                env=mcp_env,
                timeout=180,
            )
            return rcode, scrub(so), scrub(se)

        rcode, so, _ = computer("rclone-copy --help; rclone-read --help", "help")
        ev["computer_tool_listing"] = so
        sfs = quote(f"lab-src:{src}")
        rawdir = out / "raw"
        rawdir.mkdir(exist_ok=True)
        # finding: first attempt at the memory backend used root-level paths
        att = []
        for f in FILES[:1]:
            att.append(
                f"echo '@@ copy'\nrclone-copy operations-copyfile --src-fs {sfs} --src-remote {f} --dst-fs lab-memory-root: --dst-remote {f}\n"
            )
        att.append(
            "echo '@@ list'\nrclone-read operations-list --fs lab-memory-root: --remote ''\necho '@@ stat'\nrclone-read operations-stat --fs lab-memory-root: --remote flute.mid\necho '@@ size'\nrclone-read operations-size --fs lab-memory-root:\n"
        )
        rcode, so, se = computer("".join(att), "memory-root")
        (rawdir / "memory-root-attempt-output.txt").write_text(so)
        ev["memory_root_path_attempt"] = {
            "summary": "Copying flute.mid to the ROOT of the memory remote (lab-memory-root:flute.mid) was accepted, but listing showed it as a bucket (IsDir, Size -1) even though stat read 242 bytes. The memory backend is bucket-based; the lab therefore delivers into the bucket scores/.",
            "raw": parse_sections(so),
        }
        for b in backends(lab):
            lines = []

            def call(label, cmd):
                lines.append(f"echo '@@ {label}'\n{cmd}\n")

            for f in FILES:
                call(
                    f"copy-{f}",
                    f"rclone-copy operations-copyfile --src-fs {sfs} --src-remote {f} --dst-fs {quote(b['dst'])} --dst-remote {b['paths'][f]}",
                )
            call(
                "list",
                "rclone-read operations-list --json "
                + quote(
                    json.dumps(
                        {
                            "fs": b["dst"],
                            "remote": "",
                            "opt": json.dumps({"recurse": True, "showHash": True}),
                        }
                    )
                ),
            )
            for f in FILES:
                call(
                    f"stat-{f}",
                    f"rclone-read operations-stat --fs {quote(b['dst'])} --remote {b['paths'][f]}",
                )
                call(
                    f"sha256-{f}",
                    f"rclone-read operations-hashsumfile --fs {quote(b['dst'])} --remote {b['paths'][f]} --hash-type sha256 --download",
                )
            call("size", f"rclone-read operations-size --fs {quote(b['dst'])}")
            if b.get("check", True):
                call(
                    "check",
                    f"rclone-read operations-check --src-fs {sfs} --dst-fs {quote(b['dst'])} --download --one-way --match --combined",
                )
            script = "".join(lines)
            rcode, so, se = computer(script, b["id"])
            (rawdir / f"{b['id']}-computer-script.sh").write_text(scrub(script))
            (rawdir / f"{b['id']}-computer-output.txt").write_text(
                so + ("\n[stderr]\n" + se if se.strip() else "")
            )
            sec = parse_sections(so)
            # --- separate read path (not the rcd, not MCP)
            sep, standalone = {}, {}
            for f in FILES:
                target = join(b["dst"], b["paths"][f])
                if b.get("memory"):
                    u = url + "/[" + b["dst"] + "]/" + b["paths"][f]
                    data = urllib.request.urlopen(
                        urllib.request.Request(u, headers={"Authorization": auth}),
                        timeout=20,
                    ).read()
                    how = "HTTP GET on rcd --rc-serve /[lab-memory:scores]/ path (curl-style), hashed natively in Python"
                else:
                    rc_, data, e = sh(["rclone", "cat", target], env=renv)
                    how = "separate `rclone cat` process (same config file, no rcd, no MCP), hashed natively in Python"
                    if rc_:
                        data = b""
                standalone[f] = {
                    "how": how,
                    "bytes": len(data),
                    "sha256": sha(data),
                    "b64": base64.b64encode(data).decode(),
                }
            if b.get("memory"):
                rc_, o, e = sh(["rclone", "lsf", "lab-memory:"], env=renv)
                sep["standalone_memory_observation"] = {
                    "cmd": "rclone lsf lab-memory:  (separate process)",
                    "listing": o.decode().split(),
                    "note": "a second process has its own empty memory remote; the objects exist only inside rcd",
                }
            if b.get("crypt"):
                inv0 = physical_inventory(lab, b)
                names = [Path(i["path"]).name for i in inv0]
                rc_, o, e = sh(
                    ["rclone", "cryptdecode", "--reverse", "lab-crypt:", *FILES],
                    env=renv,
                )
                sep["crypt_names"] = {
                    "cmd": "rclone cryptdecode --reverse lab-crypt: <plain names>",
                    "mapping": scrub(o).strip().splitlines(),
                }
            inv = physical_inventory(lab, b)
            nat = native_sha(lab, b, inv)
            files = []
            for f in FILES:
                mh = sec.get(f"sha256-{f}", {}).get("hash")
                st = sec.get(f"stat-{f}", {}).get("item") or {}
                row = {
                    "name": f,
                    "remote_path": b["paths"][f],
                    "source_bytes": len((src / f).read_bytes()),
                    "source_sha256": SRC_SHA[f],
                    "copy_ok": sec.get(f"copy-{f}") == {},
                    "copy_reply": sec.get(f"copy-{f}"),
                    "mcp_stat_bytes": st.get("Size"),
                    "mcp_sha256": mh,
                    "separate_read": standalone[f]["how"],
                    "separate_bytes": standalone[f]["bytes"],
                    "separate_sha256": standalone[f]["sha256"],
                    "native_sha256": nat[f],
                }
                row["verified_mcp"] = mh == SRC_SHA[f]
                row["verified_separate"] = standalone[f]["sha256"] == SRC_SHA[f]
                row["verified_native"] = (
                    None if nat[f] is None else nat[f] == SRC_SHA[f]
                )
                row["arrived"] = (
                    row["verified_mcp"]
                    and row["verified_separate"]
                    and row["verified_native"] is not False
                )
                files.append(row)
            n_ok = sum(r["arrived"] for r in files)
            status = (
                "complete" if n_ok == len(FILES) else ("partial" if n_ok else "failed")
            )
            lst = (
                sec.get("list", {}).get("list", [])
                if isinstance(sec.get("list"), dict)
                else []
            )
            entry = {
                "id": b["id"],
                "rclone_type": b["rtype"],
                "remote": b["remote"],
                "what": b["what"],
                "status": status,
                "files_arrived": n_ok,
                "files_expected": len(FILES),
                "adapter": "Computer -> upstream MCP rclone-mcp over stdio -> rclone rcd RC API on loopback HTTP",
                "write_server": "rclone-copy",
                "read_server": "rclone-read",
                "separate_read_path": standalone[FILES[0]]["how"],
                "mcp_list": [
                    {k: i.get(k) for k in ("Path", "Size", "IsDir", "Hashes")}
                    for i in lst
                ],
                "mcp_size": sec.get("size"),
                "mcp_check": sec.get("check"),
                "files": files,
                "physical": inv,
                "physical_roots": b["roots"],
                "extra": sep,
            }
            if any(not r["copy_ok"] for r in files):
                entry["copy_replies"] = [r["copy_reply"] for r in files]
            ev["backends"].append(entry)
            for f in FILES:
                d = out / "retrieved" / b["id"]
                d.mkdir(parents=True, exist_ok=True)
                (d / f).write_bytes(base64.b64decode(standalone[f]["b64"]))
            print(
                f"{b['id']:9} {status:8} {n_ok}/{len(FILES)}  physical files: {len(inv)}",
                flush=True,
            )
    finally:
        rcd.terminate()
        try:
            rcd.wait(timeout=10)
        except subprocess.TimeoutExpired:
            rcd.kill()
    ev["rcd_log_excerpt"] = scrub((lab / "rcd.log").read_text())[-1500:]
    ev = json.loads(scrub(json.dumps(ev)))
    (out / "evidence.json").write_text(json.dumps(ev, indent=1))
    print("wrote", out / "evidence.json")
    if any(b["status"] != "complete" for b in ev["backends"]):
        raise SystemExit("One or more copies could not be verified")


if __name__ == "__main__":
    main()
