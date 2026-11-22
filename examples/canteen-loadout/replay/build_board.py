"""Build the offline operator board: python3 build_board.py balance.json OUTDIR  (inlines the data; no network)."""
import json, shutil, sys
from pathlib import Path
src = Path(sys.argv[1]); out = Path(sys.argv[2]); (out / "data").mkdir(parents=True, exist_ok=True)
data = json.loads(src.read_text())
for f in ("transfers.csv", "loaded.parquet", "balance.json"):
    shutil.copy(src.parent / f, out / "data" / f)
html = r"""<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Canteen loadout board</title><style>
:root{--bg:#f6f4ef;--ink:#1d2421;--mut:#5d6760;--card:#fff;--line:#d9d5ca;--ok:#1b7a4b;--bad:#b3341f;--acc:#24576b}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.45 system-ui,sans-serif}
main{max-width:980px;margin:0 auto;padding:20px 16px 48px}h1{font-size:1.5rem;margin:.2em 0}h2{font-size:1.05rem;margin:0 0 .6em}
p.sub{color:var(--mut);margin:0 0 16px}.bar{display:flex;flex-wrap:wrap;gap:12px;align-items:end;margin-bottom:16px}
label{font-size:.8rem;color:var(--mut);display:block}select,button{font:inherit;padding:8px 12px;border:1px solid var(--line);border-radius:8px;background:#fff;color:var(--ink);min-height:40px}
.seg{display:inline-flex;border:1px solid var(--line);border-radius:8px;overflow:hidden}.seg button{border:0;border-radius:0}.seg button[aria-pressed=true]{background:var(--acc);color:#fff}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:16px;margin-bottom:16px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:12px}
.meal{border:1px solid var(--line);border-radius:10px;padding:12px}.meal h3{margin:0 0 8px;font-size:1rem}
.row{display:flex;justify-content:space-between;font-variant-numeric:tabular-nums}.row span:first-child{color:var(--mut)}
.track{position:relative;height:14px;background:#ecebe4;border-radius:7px;margin:10px 0 6px;overflow:hidden}.fill{height:100%;background:var(--acc);transition:width .4s}
.mark{position:absolute;top:0;bottom:0;width:2px;background:var(--ink)}
.badge{display:inline-block;padding:2px 8px;border-radius:99px;font-size:.85rem;font-weight:600}.short{background:#fbe3de;color:var(--bad)}.fine{background:#dff1e6;color:var(--ok)}
.scroll{overflow-x:auto}table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line)}td.n,th.n{text-align:right}
tr.sel{background:#eef4f6}.links a{margin-right:16px;color:var(--acc)}.total{font-weight:600}
@media(max-width:520px){main{padding:12px}table{font-size:.85rem}th,td{padding:5px 3px}#all td:nth-child(2){white-space:normal}}
</style></head><body><main>
<h1>Canteen loadout board</h1>
<p class="sub">Lunch service. Ordered units from PostgreSQL, loaded units from a Parquet file, joined by a guest Python inside Computer. Works offline.</p>
<div class="bar"><div><label for="hall">Hall</label><select id="hall"></select></div>
<div><label>View</label><div class="seg" role="group" aria-label="Before or after transfers"><button id="b" aria-pressed="true">Before</button><button id="a" aria-pressed="false">After transfers</button></div></div>
<div id="state" class="badge" role="status"></div></div>
<section class="card"><h2 id="ht"></h2><div class="grid" id="meals"></div></section>
<section class="card"><h2>All halls, per meal</h2><div class="scroll"><table id="all"><thead><tr><th>Hall</th><th>Meal</th><th class="n">Ordered</th><th class="n">Loaded</th><th class="n" id="cv">Now</th><th class="n">Short</th></tr></thead><tbody></tbody></table></div></section>
<section class="card"><h2>Transfers issued by the guest</h2><table id="tr"><thead><tr><th>Meal</th><th>From</th><th>To</th><th class="n">Units</th></tr></thead><tbody></tbody></table>
<p class="links"><a href="data/transfers.csv" download>transfers.csv</a><a href="data/loaded.parquet" download>loaded.parquet</a><a href="data/balance.json" download>balance.json</a></p></section>
</main><script>
const D=__DATA__;let after=false;const $=s=>document.querySelector(s);
const halls=[...new Set(D.lines.map(l=>l.hall))].sort();
$("#hall").innerHTML=halls.map(h=>`<option>${h}</option>`).join("");$("#hall").value="Quay";
const qty=l=>after?l.final:l.loaded, short=l=>Math.max(0,l.ordered-qty(l));
function render(){const h=$("#hall").value,L=D.lines.filter(l=>l.hall===h);
$("#ht").textContent=h+" hall — "+(after?"after transfers":"as loaded");$("#cv").textContent=after?"Final":"Now";
const miss=D.lines.reduce((s,l)=>s+short(l),0);const st=$("#state");st.textContent=miss?miss+" units short across halls":"No units short";st.className="badge "+(miss?"short":"fine");
$("#meals").innerHTML=L.map(l=>{const q=qty(l),m=Math.max(l.ordered,q,1)*1.15,s=short(l);
const moves=after?(l.transfer_in?`<div class="row"><span>Transfer in</span><span>+${l.transfer_in}</span></div>`:"")+(l.transfer_out?`<div class="row"><span>Transfer out</span><span>−${l.transfer_out}</span></div>`:""):"";
return `<div class="meal"><h3>${l.meal}</h3><div class="row"><span>Ordered</span><span>${l.ordered}</span></div><div class="row"><span>${after?"Loaded originally":"Loaded"}</span><span>${l.loaded}</span></div>${moves}
<div class="track" aria-hidden="true"><div class="fill" style="width:${q/m*100}%"></div><div class="mark" style="left:${l.ordered/m*100}%"></div></div>
<div class="row total"><span>${after?"Final":"Loaded"} ${q}</span><span class="badge ${s?"short":"fine"}">${s?s+" short":(q>l.ordered?(q-l.ordered)+" spare":"exact")}</span></div></div>`}).join("");
$("#all tbody").innerHTML=D.lines.map(l=>`<tr class="${l.hall===h?"sel":""}"><td>${l.hall}</td><td>${l.meal}</td><td class="n">${l.ordered}</td><td class="n">${l.loaded}</td><td class="n">${qty(l)}</td><td class="n">${short(l)}</td></tr>`).join("");
$("#tr tbody").innerHTML=D.transfers.map(t=>`<tr><td>${t.meal}</td><td>${t.from_hall}</td><td>${t.to_hall}</td><td class="n">${t.units}</td></tr>`).join("");
$("#b").setAttribute("aria-pressed",!after);$("#a").setAttribute("aria-pressed",after)}
$("#hall").onchange=render;$("#b").onclick=()=>{after=false;render()};$("#a").onclick=()=>{after=true;render()};render();
</script></body></html>"""
(out / "index.html").write_text(html.replace("__DATA__", json.dumps(data)))
