"""Extract CuraEngine's deposited outer-wall paths for a side-by-side layer reader."""

import json
from pathlib import Path


def read_layers(factor):
    layers = []
    position = {}
    kind = ""
    with Path(f"oslo-{factor}x.gcode").open() as file:
        for line in file:
            line = line.strip()
            if line.startswith(";LAYER:"):
                index = int(line.split(":")[1])
                if index != len(layers):
                    raise ValueError("unexpected layer order")
                layers.append({"z": None, "paths": [], "extrusion_moves": 0})
                kind = ""
            elif line.startswith(";TYPE:"):
                kind = line[6:]
            elif line.startswith("M104 S0"):
                break
            elif line.startswith(("G0 ", "G1 ", "G92 ")):
                words = line.split(";")[0].split()
                moves = {t[0]: float(t[1:]) for t in words[1:] if t[0] in "XYZE"}
                after = {**position, **moves}
                if (
                    layers
                    and words[0] == "G1"
                    and ("X" in moves or "Y" in moves)
                    and moves.get("E", -1) > position.get("E", 0)
                ):
                    layer = layers[-1]
                    layer["z"] = after["Z"]
                    layer["extrusion_moves"] += 1
                    if kind == "WALL-OUTER":
                        a = [round(position[k], 2) for k in "XY"]
                        b = [round(after[k], 2) for k in "XY"]
                        paths = layer["paths"]
                        if not paths or paths[-1][-1] != a:
                            paths.append([a])
                        paths[-1].append(b)
                position = after
    return layers


editions = {str(f): read_layers(f) for f in (1, 4)}
receipts = {}
for factor, layers in editions.items():
    depositing = [layer for layer in layers if layer["extrusion_moves"]]
    walls = [layer for layer in depositing if layer["paths"]]
    receipts[factor] = {
        "slicer_layer_markers": len(layers),
        "layers_depositing_material": len(depositing),
        "layers_with_outer_wall_paths": len(walls),
        "highest_deposition_mm": max(layer["z"] for layer in depositing),
        "extrusion_moves": sum(layer["extrusion_moves"] for layer in layers),
    }
    if any(
        abs(layer["z"] - (i + 1) * 0.2) > 0.001 for i, layer in enumerate(depositing)
    ):
        raise ValueError("unexpected deposition height")
Path("layers.json").write_text(json.dumps(receipts, indent=2) + "\n")
template = Path("/recipe/layers.html").read_text()
Path("layers.html").write_text(
    template.replace("{{LAYERS}}", json.dumps(editions, separators=(",", ":")))
)
print(json.dumps(receipts))
