"""Make an offline sequence from the actual browser capture and its OPFS receipts."""
import base64
import json
from pathlib import Path

sample = Path(__file__).resolve().parent / "sample"
record = json.loads((sample / "browser.json").read_bytes())[0]
stages = [
    ("before", "Before the recount", "Computer read five medium jackets from this browser's store and wrote the first allocation. Two ushers still needed a jacket."),
    ("offline", "Count eight offline", "The wardrobe assistant saved eight medium jackets with browser networking disabled. The old allocation remained on disk, and the desk marked it stale."),
    ("after", "Reopen the fitting list", "Back online, Computer read the revised count and saved seven fittings and one spare. Both OPFS files survived a full browser process restart in the same isolated profile."),
]
data = []
for name, title, caption in stages:
    image = base64.b64encode((sample / (name + ".png")).read_bytes()).decode()
    data.append({"title": title, "caption": caption, "image": image, "state": record["stages"][name]})
page = """<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Recorded wardrobe handoff</title>
<style>*{box-sizing:border-box}body{margin:0;background:#ede9dc;color:#283529;font:16px/1.45 system-ui}main{max-width:1050px;margin:auto;padding:26px 4vw}h1{font:normal clamp(36px,6vw,68px)/1 Georgia,serif;margin:16px 0}nav{display:flex;gap:7px;flex-wrap:wrap}button{font:inherit;background:#f8f6ed;color:#314234;border:1px solid #63705d;padding:6px 10px;cursor:pointer}button[aria-pressed=true]{background:#304d3d;color:white}img{width:100%;height:auto;border:1px solid #b0ac9b}pre{overflow-wrap:anywhere;white-space:pre-wrap;font-size:13px}p{max-width:810px}small{letter-spacing:.04em}</style>
<main><small>Computer · recorded browser handoff</small><h1>Seven ushers, one jacket rack</h1><nav id="tabs" aria-label="Recorded stages"></nav><p id="caption" aria-live="polite"></p><img id="frame"><details><summary>Inspect the two stored records</summary><pre id="state"></pre></details></main>
<script type="application/json" id="capture">{{DATA}}</script><script>
const seq=JSON.parse(document.querySelector('#capture').textContent),tabs=document.querySelector('#tabs');
function select(i){const x=seq[i],im=document.querySelector('#frame');im.src='data:image/png;base64,'+x.image;im.alt=x.title+' in the recorded wardrobe desk';document.querySelector('#caption').textContent=x.caption;document.querySelector('#state').textContent=JSON.stringify(x.state,null,2);[...tabs.children].forEach((b,n)=>b.setAttribute('aria-pressed',String(i===n)));}
seq.forEach((x,i)=>{const b=document.createElement('button');b.textContent=x.title;b.onclick=()=>select(i);tabs.append(b);});select(0);
</script></html>"""
(sample / "wardrobe.html").write_text(page.replace("{{DATA}}", json.dumps(data, separators=(",", ":")).replace("<", "\\u003c")))
