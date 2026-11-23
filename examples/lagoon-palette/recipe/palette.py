"""Publish native design swatches and an offline style sheet inside Computer."""
import base64
import html
import json
from pathlib import Path
import struct

colors=json.loads(Path("selection.json").read_bytes())
palette=["GIMP Palette","Name: Venetian Lagoon","Columns: 6","# Contains modified Copernicus Sentinel data (2024)."]
css=[":root {"]
ase=bytearray(b"ASEF"+struct.pack(">HHI",1,0,len(colors)))
for color in colors:
    r,g,b=color["rgb"];name=color["name"]
    if any(v not in range(256) for v in [r,g,b]):raise ValueError("invalid RGB")
    color["hex"]="#%02X%02X%02X"%(r,g,b)
    palette.append(f"{r:3} {g:3} {b:3}\t{name}")
    slug="-".join(name.lower().split())
    css.append("  --"+slug+": "+color["hex"]+";")
    utf16=(name+"\0").encode("utf-16-be")
    body=struct.pack(">H",len(utf16)//2)+utf16+b"RGB "+struct.pack(">fffH",r/255,g/255,b/255,2)
    ase.extend(struct.pack(">HI",1,len(body))+body)
css.append("}")
Path("venetian-lagoon.gpl").write_text("\n".join(palette)+"\n")
Path("venetian-lagoon.css").write_text("\n".join(css)+"\n")
Path("venetian-lagoon.ase").write_bytes(ase)
payload={"colors":colors,"pictures":{},"files":{}}
for site in dict.fromkeys(c["site"] for c in colors):
    payload["pictures"][site]=base64.b64encode((Path("/scenes")/(site+".png")).read_bytes()).decode()
for ext in ["ase","gpl","css"]:
    payload["files"][ext]=base64.b64encode(Path("venetian-lagoon."+ext).read_bytes()).decode()
Path("palette.html").write_text(Path("/recipe/palette.html").read_text().replace("{{DATA}}",json.dumps(payload,separators=(",",":")).replace("<","\\u003c")))
print(json.dumps({"swatches":len(colors),"exports":["ase","gpl","css"]}))
