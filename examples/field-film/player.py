"""Build a chapter menu that also works when the page is opened from disk."""
import base64
import html
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
chapters = json.loads((out / 'chapters.json').read_text())
buttons = []
for chapter in chapters:
    t = int(chapter['start'])
    buttons.append(f'<button type="button" data-time="{chapter["start"]}"><time>{t//60}:{t%60:02}</time> '
                   f'{html.escape(chapter["title"])}</button>')
caption = base64.b64encode((out / 'captions.vtt').read_bytes()).decode()
page = """<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Computer field film</title><style>
*{box-sizing:border-box}body{margin:0;background:#f6f2e9;color:#182c31;font:17px/1.5 system-ui,sans-serif}
main{max-width:1150px;margin:auto;padding:clamp(14px,4vw,48px)}h1{font:normal clamp(38px,7vw,76px)/1.02 Georgia,serif;margin:0 0 .18em}
p{margin:.5rem 0 1.2rem}video{display:block;width:100%;height:auto;background:#182c31}
nav{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,270px),1fr));gap:1px;margin:1.2rem 0;background:#b6bbaa;border:1px solid #b6bbaa}
button{background:#f6f2e9;color:#182c31;border:0;text-align:left;padding:1rem;font:inherit;cursor:pointer}button:hover,button[aria-current=true]{background:#e0e7db}
button:focus-visible,a:focus-visible{outline:3px solid #87532c;outline-offset:-3px}time{color:#834925;font-variant-numeric:tabular-nums;font-weight:650;padding-right:.55em}a{color:#254d40}footer{display:flex;flex-wrap:wrap;gap:1.5em}</style>
<main><h1>Computer field film</h1><p>Follow ferry records, satellite windows, a designer’s palette, terrain, a wardrobe recount and a score through Computer.</p>
<video controls playsinline preload="metadata" poster="poster.jpg" src="computer.mp4" aria-label="Six narrated Computer demonstrations"><track kind="captions" srclang="en" label="English" src="data:text/vtt;base64,CAPTION"></video>
<nav aria-label="Film chapters">BUTTONS</nav><footer><a href="computer.mp4">Open the MP4</a><a href="transcript.md">Read the transcript</a><a href="captions.vtt">Open English captions</a></footer></main>
<script>const video=document.querySelector('video'),buttons=[...document.querySelectorAll('button')];
for(const b of buttons)b.addEventListener('click',()=>{video.currentTime=Number(b.dataset.time);video.play().catch(()=>{});});
function update(){const current=buttons.filter(b=>Number(b.dataset.time)<=video.currentTime+0.02).pop();for(const b of buttons)b.setAttribute('aria-current',String(b===current));}
video.addEventListener('timeupdate',update);update();</script></html>"""
(out / 'index.html').write_text(page.replace('BUTTONS', ''.join(buttons)).replace('CAPTION', caption))
