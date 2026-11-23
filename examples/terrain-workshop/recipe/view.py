"""Package both original STL files with the local Three.js renderer."""

import base64
import json
from pathlib import Path

models = json.loads(Path("models.json").read_text())["models"]
payload = []
for model in models:
    payload.append(
        {
            "file": model["file"],
            "factor": model["vertical_exaggeration"],
            "width": model["width_mm"],
            "depth": model["depth_mm"],
            "height": model["height_mm"],
            "base64": base64.b64encode(Path(model["file"]).read_bytes()).decode(),
        }
    )

template = Path("/recipe/model.html").read_text()
viewer = Path("/viewer/viewer.bundle.js").read_text()
html = template.replace("{{MODELS}}", json.dumps(payload, separators=(",", ":")))
Path("model.html").write_text(html.replace("{{VIEWER}}", viewer))
