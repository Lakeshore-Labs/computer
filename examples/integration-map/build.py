"""Build an offline map from the current checkout's evidence and real preview images."""

import base64, hashlib, io, json
from pathlib import Path
from PIL import Image

here = Path(__file__).resolve().parent
routes = json.loads((here / "routes.json").read_text())
images = {}
for r in routes:
    evidence = here.parent / r["receipt"]
    raw = evidence.read_bytes()
    json.loads(raw)
    r["evidence_bytes"] = base64.b64encode(raw).decode()
    r["receipt_sha256"] = hashlib.sha256(evidence.read_bytes()).hexdigest()
    cover = r["cover"]
    if cover not in images:
        with Image.open(here.parent / cover) as im:
            im.thumbnail((840, 530))
            jpg = io.BytesIO()
            im.convert("RGB").save(jpg, format="JPEG", quality=82)
        images[cover] = base64.b64encode(jpg.getvalue()).decode()
data = json.dumps({"routes": routes, "images": images}, ensure_ascii=False).replace(
    "<", r"\u003c"
)
(here / "sample/map.html").write_text(
    (here / "map.html").read_text().replace("{{DATA}}", data)
)
