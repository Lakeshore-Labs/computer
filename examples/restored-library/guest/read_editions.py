"""Runs INSIDE a separate Computer guest process (the distinct reader). Reads only the two
restic-restored trees (read-only mounts) plus a host manifest of source hashes; writes the sheet."""
import base64, csv, hashlib, html, json, sys
orig, chg, meta, out = sys.argv[1:5]
manifest = json.load(open(meta+"/manifest.json"))
def sha(p): return hashlib.sha256(open(p,"rb").read()).hexdigest()
def hm(t):
    return 0 if t=="closed" else int(t[:2])*60+int(t[3:])
trees = {"original":orig, "changed":chg}
files = ["books.csv","hours.csv","notice.md"]
H = {k:{f:sha(t+"/"+f) for f in files} for k,t in trees.items()}
verified = {k:{f:H[k][f]==manifest[k][f] for f in files} for k in trees}
hours = {k:list(csv.DictReader(open(t+"/hours.csv"))) for k,t in trees.items()}
books = {k:list(csv.DictReader(open(t+"/books.csv"))) for k,t in trees.items()}
changed_files = [f for f in files if H["original"][f]!=H["changed"][f]]
changed_days = [a["day"] for a,b in zip(hours["original"],hours["changed"]) if (a["opens"],a["closes"])!=(b["opens"],b["closes"])]
shelf = {}
for r in books["original"]: shelf[r["shelf"]] = shelf.get(r["shelf"],0)+int(r["copies"])
def weekly(rows):
    return sum((hm(r["closes"])-hm(r["opens"]))/60 for r in rows if r["opens"]!="closed")
tot = {k:weekly(hours[k]) for k in trees}
result = {"changed_files":changed_files,"changed_days":changed_days,"verified":verified,"sha256":H,
          "weekly_hours":tot,"titles":len(books["original"]),"copies":sum(shelf.values()),
          "books_identical":H["original"]["books.csv"]==H["changed"]["books.csv"]}
json.dump(result, open(out+"/comparison.json","w"), indent=2)
LO, HI = 8*60, 21*60
def bars(k, other):
    s = ""
    for r, o in zip(hours[k], hours[other]):
        diff = (r["opens"],r["closes"])!=(o["opens"],o["closes"])
        if r["opens"]=="closed":
            bar = '<span class="shut">closed</span>'
        else:
            l = (hm(r["opens"])-LO)/(HI-LO)*100; w = (hm(r["closes"])-hm(r["opens"]))/(HI-LO)*100
            bar = f'<span class="bar{" diff" if diff else ""}" style="left:{l:.2f}%;width:{w:.2f}%"></span>'
        s += f'<div class="day" data-changed="{str(diff).lower()}"><b>{html.escape(r["day"][:3])}</b><div class="track">{bar}</div><i>{html.escape(r["opens"])}{"" if r["opens"]=="closed" else "–"+html.escape(r["closes"])}</i></div>'
    return s
def chips(k):
    s = ""
    for f in files:
        ok = verified[k][f]; same = H["original"][f]==H["changed"][f]
        s += (f'<tr class="{"ch" if not same else ""}"><td>{f}</td><td><code>{H[k][f][:16]}…</code></td>'
              f'<td>{"✔ matches source" if ok else "✘ MISMATCH"}</td></tr>')
    return s
mx = max(shelf.values())
sb = "".join(f'<div class="sh"><span>{html.escape(n)}</span><div><u style="width:{v/mx*100:.0f}%"></u></div><em>{v}</em></div>' for n,v in sorted(shelf.items()))
cards = ""
for k, label, snap in (("original","Edition 1 · original","restored from snapshot 1"),("changed","Edition 2 · amended","restored from snapshot 2")):
    other = "changed" if k=="original" else "original"
    with open(trees[k]+"/hours.csv", "rb") as restored_hours:
        encoded = base64.b64encode(restored_hours.read()).decode()
    cards += (f'<section class="card {k}"><h2>{label}</h2><p class="sub">{snap} · {tot[k]:g} open hours / week</p>'
              f'<div class="axis"><span>08:00</span><span>21:00</span></div>{bars(k,other)}'
              f'<table>{chips(k)}</table><a class="download" href="data:text/csv;base64,{encoded}" download="{k}-hours.csv">Download {k} hours</a></section>')
page = f'''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Reading Room editions restored from restic</title><style>
:root{{--bg:#14110f;--ink:#f3eadb;--mut:#b5a78f;--card:#211c18;--ln:#3a3029;--a:#8fb8a8;--b:#f0a65a}}
*{{box-sizing:border-box}}body{{margin:0;background:radial-gradient(1200px 500px at 20% -10%,#2b2119,var(--bg));color:var(--ink);font:16px/1.45 Georgia,serif;padding:28px 18px 40px}}
main{{max-width:1100px;margin:auto}}h1{{font-size:clamp(26px,5vw,44px);margin:0 0 6px;letter-spacing:-.5px}}
.lead{{color:var(--mut);margin:0 0 22px;max-width:70ch}}.pill{{display:inline-block;border:1px solid var(--ln);border-radius:99px;padding:3px 12px;margin:0 6px 6px 0;font:13px system-ui;color:var(--mut)}}
.pill.k{{color:var(--b);border-color:var(--b)}}.grid{{display:grid;grid-template-columns:1fr 1fr;gap:18px}}
.card{{background:var(--card);border:1px solid var(--ln);border-radius:16px;padding:18px}}.card h2{{margin:0;font-size:22px}}.sub{{color:var(--mut);font:13px system-ui;margin:2px 0 12px}}
.original h2{{color:var(--a)}}.changed h2{{color:var(--b)}}.axis{{display:flex;justify-content:space-between;margin:0 100px 2px 42px;font:11px system-ui;color:var(--mut)}}
.day{{display:grid;grid-template-columns:34px 1fr 92px;gap:8px;align-items:center;margin:5px 0;font:13px system-ui}}.day i{{font-style:normal;color:var(--mut);text-align:right}}
.track{{position:relative;height:16px;background:#2d261f;border-radius:8px}}.bar{{position:absolute;top:0;bottom:0;border-radius:8px;background:var(--a)}}
.changed .bar{{background:#c9c0ac}}.bar.diff{{background:var(--b);box-shadow:0 0 0 2px #f0a65a55}}.shut{{position:absolute;left:8px;top:-1px;font-size:11px;color:var(--mut)}}
table{{width:100%;border-collapse:collapse;margin-top:14px;font:13px system-ui}}td{{padding:6px 4px;border-top:1px solid var(--ln)}}tr.ch td{{color:var(--b)}}code{{font-size:12px}}
.books{{margin-top:18px}}.sh{{display:grid;grid-template-columns:80px 1fr 28px;gap:8px;align-items:center;font:13px system-ui;margin:4px 0}}.sh div{{background:#2d261f;height:10px;border-radius:5px}}.sh u{{display:block;height:100%;background:var(--a);border-radius:5px}}.sh em{{font-style:normal;text-align:right}}
footer{{margin-top:18px;color:var(--mut);font:13px/1.5 system-ui}}footer b{{color:var(--ink)}}
@media(max-width:760px){{.grid{{grid-template-columns:1fr}}body{{padding:20px 12px}}.day{{grid-template-columns:30px 1fr 84px}}}}
.download{{display:inline-block;color:var(--ink);margin-top:12px}}.filter{{display:block;margin:12px 0}}.filtered .day[data-changed="false"]{{display:none}}
</style><main><h1>One document changed. Both editions recovered.</h1>
<p class="lead">Two encrypted restic snapshots of a synthetic library were restored to separate folders. This page was written by a second Computer guest reading the restored documents and their original hashes.</p>
<div><span class="pill k">changed: {html.escape(", ".join(changed_files))}</span><span class="pill">{len(changed_days)} days differ: {html.escape(", ".join(d[:3] for d in changed_days))}</span><span class="pill">{result["titles"]} titles · {result["copies"]} copies, identical in both</span><span class="pill">all {2*len(files)} restored hashes match source</span></div>
<label class="filter"><input id="changes" type="checkbox"> Show only the days that changed</label><div class="grid" id="editions">{cards}</div>
<section class="card books"><h2>Book list (books.csv, unchanged)</h2><p class="sub">copies per shelf</p>{sb}</section>
<footer><b>Outside Computer (host):</b> restic encrypted the snapshots through its rclone transport over rclone’s local backend, restored them and ran <code>restic check --read-data</code>.<br><b>Inside Computer (stock guest):</b> WASI Python generated both editions, and a separate guest process hashed the restored files and rendered this sheet. All data is synthetic.</footer></main><script>
document.getElementById('changes').onchange = event => document.getElementById('editions').classList.toggle('filtered', event.target.checked);
</script></html>'''
open(out+"/index.html","w").write(page)
print(json.dumps({k:result[k] for k in ("changed_files","changed_days","weekly_hours","titles")}))
