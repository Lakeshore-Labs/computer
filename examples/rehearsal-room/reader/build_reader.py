#!/usr/bin/env python3
"""Independent rehearsal-stand reader builder.

  external engraver : Verovio (pip `verovio`) -> SVG from the ORIGINAL MusicXML
  external decoder  : mido -> sounding pitches/timing from the ORIGINAL .mid
  this script       : glue, cross-checks, hashes, synthesized WAV, offline HTML

usage:
  build_reader.py --parts ../run1/parts --source ../source/score.csv --out site \
                  --computer-json ../run1/computer.json
Exit status is non-zero if any proof check fails (site + proof are still written).
"""
import argparse, base64, re, csv, hashlib, json, math, os, shutil, struct, sys, wave
import xml.etree.ElementTree as ET
import mido, verovio

PARTS = {  # key: (label, transposition semitones written-over-sounding)
    "flute": ("Flute", 0),
    "clarinet": ("B♭ Clarinet", 2),
}
SAMPLE_RATE = 16000
STEP_PC = {"C": 0, "D": 2, "E": 4, "F": 5, "G": 7, "A": 9, "B": 11}

def sha256(path):
    return hashlib.sha256(open(path, "rb").read()).hexdigest()

# ---------------------------------------------------------------- source CSV
def read_source(path, bpm):
    rows = list(csv.DictReader(open(path)))
    t, out = 0.0, []
    for i, r in enumerate(rows):
        b = float(r["beats"])
        out.append(dict(i=i, measure=int(r["measure"]), pitch=int(r["concert_midi"]),
                        beats=b, onset_beat=t, onset=t * 60 / bpm, dur=b * 60 / bpm))
        t += b
    return out

# ---------------------------------------------------------------- MIDI (mido)
def decode_midi(path):
    mf = mido.MidiFile(path)
    ppq = mf.ticks_per_beat
    tempos, ts, programs, names = [], [], [], []
    notes, open_ = [], {}
    for tr in mf.tracks:
        tick = 0
        for m in tr:
            tick += m.time
            if m.type == "set_tempo": tempos.append((tick, m.tempo))
            elif m.type == "time_signature": ts.append(f"{m.numerator}/{m.denominator}")
            elif m.type == "track_name": names.append(m.name)
            elif m.type == "program_change": programs.append(m.program)
            elif m.type == "note_on" and m.velocity > 0: open_[(m.channel, m.note)] = (tick, m.velocity)
            elif m.type in ("note_off", "note_on"):
                k = (m.channel, m.note)
                if k in open_:
                    t0, v = open_.pop(k); notes.append((t0, tick, m.note, v))
    tempos = sorted(set(tempos)) or [(0, 500000)]  # MIDI default 120 bpm if none
    if tempos[0][0] > 0: tempos.insert(0, (0, 500000))
    def secs(tk):
        s, last_t, last_us = 0.0, 0, tempos[0][1]
        for t, us in tempos:
            if t >= tk: break
            s += (t - last_t) / ppq * last_us / 1e6; last_t, last_us = t, us
        return s + (tk - last_t) / ppq * last_us / 1e6
    notes.sort()
    ev = [dict(i=i, pitch=p, vel=v, tick=a, onset=secs(a), dur=secs(b) - secs(a), beats=(b - a) / ppq)
          for i, (a, b, p, v) in enumerate(notes)]
    return dict(ppq=ppq, bpm=[round(60e6 / us, 3) for _, us in tempos], time_sig=ts,
                programs=programs, track_names=names, events=ev, unclosed=len(open_))

# ------------------------------------------------------ MusicXML (own parse)
def parse_musicxml(path):
    root = ET.parse(path).getroot()
    div, trans, bpm, ts, fifths, time_beat = 1, 0, None, None, None, 0.0
    prog = None
    for e in root.iter("midi-program"): prog = int(e.text)
    title = (root.findtext("work/work-title") or "")
    pname = root.findtext(".//part-name") or ""
    notes = []
    for meas in root.iter("measure"):
        for el in meas:
            if el.tag == "attributes":
                if el.findtext("divisions"): div = int(el.findtext("divisions"))
                if el.find("transpose") is not None: trans = int(el.findtext("transpose/chromatic") or 0)
                if el.find("time") is not None: ts = f'{el.findtext("time/beats")}/{el.findtext("time/beat-type")}'
                if el.find("key") is not None: fifths = int(el.findtext("key/fifths"))
            elif el.tag == "direction":
                s = el.find("sound")
                if s is not None and s.get("tempo"): bpm = float(s.get("tempo"))
            elif el.tag == "note":
                d = int(el.findtext("duration")) / div
                if el.find("rest") is None:
                    p = el.find("pitch")
                    written = 12 * (int(p.findtext("octave")) + 1) + STEP_PC[p.findtext("step")] + int(float(p.findtext("alter") or 0))
                    notes.append(dict(i=len(notes), measure=int(meas.get("number")), written=written,
                                      pitch=written + trans, beats=d, onset_beat=time_beat))
                time_beat += d
    for n in notes: n["onset"] = n["onset_beat"] * 60 / bpm
    return dict(title=title, part_name=pname, divisions=div, transpose_chromatic=trans, bpm=bpm,
                time_sig=ts, fifths=fifths, midi_program_1based=prog, notes=notes)

# ---------------------------------------------------------- Verovio engraving
def engrave(xml_path, seed):
    """-> dict layout -> svg text, plus verovio timemap + per-note ids."""
    xml = open(xml_path, encoding="utf-8").read()
    res = {}
    for layout, (width, scale) in {"wide": (1650, 42), "narrow": (1300, 42)}.items():
        tk = verovio.toolkit()
        tk.setOptions(dict(pageWidth=width, scale=scale, adjustPageHeight=True, svgViewBox=True,
                           header="none", footer="none", pageMarginLeft=60, pageMarginRight=60,
                           pageMarginTop=40, pageMarginBottom=40, xmlIdSeed=seed + (1 if layout == "narrow" else 0),
                           svgRemoveXlink=True, svgHtml5=True, breaks="auto", spacingStaff=8))
        if not tk.loadData(xml): raise SystemExit(f"verovio failed to load {xml_path}")
        res[layout] = dict(svg=tk.renderToSVG(1), pages=tk.getPageCount())
        if layout == "wide":
            tm = tk.renderToTimemap(dict(includeRests=False))
            res["timemap"] = [dict(ms=e["tstamp"], on=e.get("on", [])) for e in tm if e.get("on")]
            res["verovio_version"] = tk.getVersion()
    return res

# ------------------------------------------------------------- synthesized WAV
HARM = {"flute": [(1, 1.0), (2, 0.25), (3, 0.08)],
        "clarinet": [(1, 1.0), (3, 0.55), (5, 0.30), (7, 0.14), (9, 0.07)]}

def synth_wav(path, events, key):
    """Plain additive synthesis of decoded MIDI events (NOT a sampler / recording)."""
    total = max(e["onset"] + e["dur"] for e in events) + 0.5
    buf = [0.0] * int(total * SAMPLE_RATE)
    for e in events:
        f0 = 440 * 2 ** ((e["pitch"] - 69) / 12)
        n0, n = int(e["onset"] * SAMPLE_RATE), int((e["dur"] * 0.97 + 0.12) * SAMPLE_RATE)
        atk, rel = 0.045, 0.10
        for k in range(n):
            t = k / SAMPLE_RATE
            env = min(1, t / atk) * (1 if t < e["dur"] * 0.97 else max(0, 1 - (t - e["dur"] * 0.97) / rel))
            vib = 1 + 0.004 * math.sin(2 * math.pi * 5 * t) * min(1, t / 0.4)
            s = sum(a * math.sin(2 * math.pi * f0 * h * vib * t) for h, a in HARM[key])
            if n0 + k < len(buf): buf[n0 + k] += 0.16 * env * s
    pk = max(abs(x) for x in buf) or 1
    with wave.open(path, "wb") as w:
        w.setnchannels(1); w.setsampwidth(2); w.setframerate(SAMPLE_RATE)
        w.writeframes(b"".join(struct.pack("<h", int(32000 * x / max(pk, 1))) for x in buf))
    return dict(duration=total, peak=round(pk, 3))

# ------------------------------------------------------------------- main
def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--parts", required=True); ap.add_argument("--source", required=True)
    ap.add_argument("--out", required=True); ap.add_argument("--expect-bpm", type=float, default=96)
    ap.add_argument("--expect-measures", type=int, default=8)
    ap.add_argument("--computer-json", required=True)
    a = ap.parse_args()
    here = os.path.dirname(os.path.abspath(__file__))
    out = os.path.abspath(a.out)
    os.makedirs(out, exist_ok=False)
    for d in ("downloads", "audio"): os.makedirs(f"{out}/{d}")

    checks = []
    def check(name, ok, detail=""):
        checks.append(dict(name=name, ok=bool(ok), detail=str(detail)))

    src = read_source(a.source, a.expect_bpm)
    nrows = len(src)
    check("source rows counted from CSV (not hardcoded)", nrows > 0, f"{nrows} notes")
    meas_sum = {}
    for r in src: meas_sum[r["measure"]] = meas_sum.get(r["measure"], 0) + r["beats"]
    check("source: every measure sums to 4 beats", all(v == 4 for v in meas_sum.values()), meas_sum)
    check(f"source: {a.expect_measures} measures", len(meas_sum) == a.expect_measures, f"{len(meas_sum)} found")
    total_dur = sum(r["dur"] for r in src)

    ledger = {}
    if not os.path.isfile(a.computer_json):
        raise SystemExit("Missing Computer hash receipt")
    else:
        for k, v in json.load(open(a.computer_json)).get("parts", {}).items():
            for line in v.strip().splitlines():
                h, fn = line.split(None, 1); ledger[fn.strip()] = h

    if len(ledger) != 4:
        raise SystemExit("Expected four Computer output hashes")
    parts, proof_parts = {}, {}
    for pi, (key, (label, shift)) in enumerate(PARTS.items()):
        xp, mp = f"{a.parts}/{key}.musicxml", f"{a.parts}/{key}.mid"
        for p in (xp, mp):
            if not os.path.exists(p): raise SystemExit(f"missing input {p}")
            shutil.copy(p, f"{out}/downloads/{os.path.basename(p)}")
        hx, hm = sha256(xp), sha256(mp)
        if ledger:
            for fn, h in ((f"{key}.musicxml", hx), (f"{key}.mid", hm)):
                check(f"{fn} sha256 matches computer.json", ledger.get(fn) == h)
        md, xd = decode_midi(mp), parse_musicxml(xp)
        eng = engrave(xp, 1000 * (pi + 1))
        ev = md["events"]

        check(f"{key}: MIDI note count == CSV rows", len(ev) == nrows, f"{len(ev)} vs {nrows}")
        check(f"{key}: MusicXML note count == CSV rows", len(xd["notes"]) == nrows, len(xd["notes"]))
        check(f"{key}: MIDI pitches == CSV concert_midi", [e["pitch"] for e in ev] == [r["pitch"] for r in src])
        check(f"{key}: MusicXML sounding pitch (written + transpose) == MIDI",
              [n["pitch"] for n in xd["notes"]] == [e["pitch"] for e in ev])
        wr_off = {n["written"] - n["pitch"] for n in xd["notes"]}
        check(f"{key}: written - sounding offset == {shift:+d} semitones", wr_off == {shift}, sorted(wr_off))
        check(f"{key}: MusicXML transpose chromatic == {-shift}", xd["transpose_chromatic"] == -shift, xd["transpose_chromatic"])
        check(f"{key}: MIDI tempo == {a.expect_bpm}", md["bpm"] == [a.expect_bpm], md["bpm"])
        check(f"{key}: MusicXML tempo == {a.expect_bpm}", xd["bpm"] == a.expect_bpm, xd["bpm"])
        check(f"{key}: time signature 4/4 in MIDI and MusicXML", md["time_sig"] == ["4/4"] and xd["time_sig"] == "4/4")
        want_prog = {"flute": 73, "clarinet": 71}[key]
        check(f"{key}: MIDI program {want_prog} (zero-based)", md["programs"] == [want_prog], md["programs"])
        check(f"{key}: MusicXML midi-program is 1-based {want_prog+1}", xd["midi_program_1based"] == want_prog + 1, xd["midi_program_1based"])
        dt_csv = max(abs(e["onset"] - r["onset"]) + abs(e["dur"] - r["dur"]) for e, r in zip(ev, src))
        dt_xml = max(abs(e["onset"] - n["onset"]) for e, n in zip(ev, xd["notes"]))
        check(f"{key}: MIDI onsets/durations match CSV beat grid @ {a.expect_bpm} (<1ms)", dt_csv < 1e-3, f"max dev {dt_csv*1000:.4f} ms")
        check(f"{key}: MIDI onsets match MusicXML grid (<1ms)", dt_xml < 1e-3, f"max dev {dt_xml*1000:.4f} ms")
        # engraver cross-check: Verovio timemap order/time vs mido
        tm = eng["timemap"]; vids = [i for e in tm for i in e["on"]]
        check(f"{key}: Verovio engraved note count == MIDI", len(vids) == len(ev), f"{len(vids)}")
        dt_v = max((abs(e["ms"] / 1000 - ev[j]["onset"]) for j, e in enumerate(tm) if j < len(ev)), default=9)
        check(f"{key}: Verovio timemap onsets match mido (<1ms)", dt_v < 1e-3, f"max dev {dt_v*1000:.4f} ms")
        for lay in ("wide", "narrow"):
            n = len(re.findall(r'(?<!-)class="note"', eng[lay]["svg"]))
            check(f"{key}: {lay} SVG has {len(ev)} note glyph groups, 1 page", n == len(ev) and eng[lay]["pages"] == 1, f"{n} notes, {eng[lay]['pages']} pages")
        wav = f"{out}/audio/{key}-synth.wav"
        wi = synth_wav(wav, ev, key)
        parts[key] = dict(key=key, label=label, shift=shift, program=md["programs"][0], title=xd["title"],
                          part_name=xd["part_name"], fifths=xd["fifths"],
                          events=[dict(i=e["i"], p=e["pitch"], w=e["pitch"] + shift, t=round(e["onset"], 4), d=round(e["dur"], 4)) for e in ev],
                          svg_wide=eng["wide"]["svg"], svg_narrow=eng["narrow"]["svg"],
                          sha_xml=hx, sha_mid=hm, wav=f"audio/{key}-synth.wav", sha_wav=sha256(wav),
                          size_xml=os.path.getsize(xp), size_mid=os.path.getsize(mp),
                          # exact original bytes, so downloads work from file:// (Chromium ignores relative download= there)
                          b64={n: base64.b64encode(open(q, "rb").read()).decode() for n, q in (("xml", xp), ("mid", mp), ("wav", wav))})
        proof_parts[key] = dict(
            files={f"{key}.musicxml": hx, f"{key}.mid": hm, f"{key}-synth.wav": sha256(wav)},
            midi=dict(ppq=md["ppq"], bpm=md["bpm"], time_sig=md["time_sig"], gm_program_zero_based=md["programs"],
                      notes=len(ev), first_onset_s=ev[0]["onset"], end_s=ev[-1]["onset"] + ev[-1]["dur"]),
            musicxml=dict(title=xd["title"], part=xd["part_name"], bpm=xd["bpm"], time_sig=xd["time_sig"],
                          transpose_chromatic=xd["transpose_chromatic"], key_fifths=xd["fifths"], notes=len(xd["notes"])),
            verovio=dict(version=eng["verovio_version"], notes_engraved=len(vids)),
            max_dev_ms=dict(midi_vs_csv=dt_csv * 1000, midi_vs_musicxml=dt_xml * 1000, midi_vs_verovio=dt_v * 1000),
            notes=[dict(n=e["i"] + 1, measure=src[e["i"]]["measure"], concert=e["pitch"], written=xd["notes"][e["i"]]["written"],
                        onset_s=round(e["onset"], 4), dur_s=round(e["dur"], 4)) for e in ev])
    a_ev, b_ev = parts["flute"]["events"], parts["clarinet"]["events"]
    check("both MIDI files carry identical sounding pitch+timing", [(e["p"], e["t"], e["d"]) for e in a_ev] == [(e["p"], e["t"], e["d"]) for e in b_ev])

    ok = all(c["ok"] for c in checks)
    proof = dict(mode="Computer score outputs",
                 source_file=os.path.basename(a.source),
                 source_sha256=sha256(a.source), rows=nrows, measures=len(meas_sum), bpm=a.expect_bpm,
                 total_duration_s=round(total_dur, 4), all_checks_pass=ok, checks=checks, parts=proof_parts)
    json.dump(proof, open(f"{out}/proof.json", "w"), indent=1)
    with open(f"{out}/proof.md", "w") as f:
        f.write(f"# Reader proof ({proof['mode']})\n\n{nrows} notes, {len(meas_sum)} measures, {a.expect_bpm} bpm, {total_dur:.3f}s. "
                f"Checks: {sum(c['ok'] for c in checks)}/{len(checks)} pass.\n\n| check | result | detail |\n|---|---|---|\n")
        for c in checks: f.write(f"| {c['name']} | {'PASS' if c['ok'] else '**FAIL**'} | {c['detail']} |\n")
        for k, pp in proof_parts.items():
            f.write(f"\n## {k}\n" + "\n".join(f"- `{h[:16]}…` {n}" for n, h in pp["files"].items()) + "\n\n| # | m | concert | written | onset s | dur s |\n|---|---|---|---|---|---|\n")
            for n in pp["notes"]: f.write(f"| {n['n']} | {n['measure']} | {n['concert']} | {n['written']} | {n['onset_s']} | {n['dur_s']} |\n")

    tpl = open(f"{here}/template.html", encoding="utf-8").read()
    data = dict(bpm=a.expect_bpm, rows=nrows, measures=len(meas_sum), duration=round(total_dur, 3),
                verovio=eng["verovio_version"], mido=str(mido.version_info),
                parts=parts, source_sha=proof["source_sha256"], all_ok=ok, n_checks=len(checks))
    html = tpl.replace("/*__DATA__*/null", json.dumps(data, ensure_ascii=False).replace("<", "\\u003c"))
    open(f"{out}/index.html", "w", encoding="utf-8").write(html)
    print(f"{'OK' if ok else 'FAILED'}: {sum(c['ok'] for c in checks)}/{len(checks)} checks -> {out}/index.html")
    for c in checks:
        if not c["ok"]: print("  FAIL", c["name"], c["detail"])
    sys.exit(0 if ok else 1)

if __name__ == "__main__":
    main()
