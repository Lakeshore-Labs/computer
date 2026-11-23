"""Generate and measure two physical scales of the Oslo terrain with Computer."""

import argparse
import json
import shlex
import subprocess
from pathlib import Path

from verify import inspect

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", required=True, type=Path)
    parser.add_argument("--python-dir", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument(
        "--live",
        action="store_true",
        help="fetch and resample the pinned 7 MB terrain tile",
    )
    parser.add_argument(
        "--viewer-bundle", type=Path, help="path to a built viewer.bundle.js"
    )
    parser.add_argument(
        "--cura",
        type=Path,
        help="path to CuraEngine 15.04.6 to create the layer reader",
    )
    args = parser.parse_args()
    computer = args.computer.resolve()
    runtime = args.python_dir.resolve()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    source = HERE / "source"
    recipe = HERE / "recipe"
    python_args = [
        "-python",
        runtime / "python.wasm",
        "-python-lib",
        runtime / "lib",
        "-timeout",
        "90s",
    ]
    if args.live:
        source = out / "source"
        source.mkdir()
        url = json.loads((recipe / "source.json").read_text())["tile"]
        subprocess.run(
            [
                computer,
                "-allow-url",
                url,
                "-mount",
                str(source) + ":/work",
                "-timeout",
                "90s",
                "-c",
                "curl -fsS " + shlex.quote(url) + " > oslo.hgt.gz",
            ],
            check=True,
        )
        subprocess.run(
            [
                computer,
                "-mount",
                str(source) + ":/work",
                "-mount",
                str(source) + ":/source:ro",
                "-mount",
                str(recipe) + ":/recipe:ro",
                *python_args,
                "-c",
                "python3 /recipe/extract.py",
            ],
            check=True,
        )
        if (source / "grid.json").read_bytes() != (
            HERE / "source/grid.json"
        ).read_bytes():
            raise ValueError("tile extraction differs from the captured grid")
    host = [
        computer,
        "-mount",
        str(out) + ":/work",
        "-mount",
        str(source) + ":/source:ro",
        "-mount",
        str(recipe) + ":/recipe:ro",
        *python_args,
    ]
    subprocess.run([*host, "-c", "python3 /recipe/model.py"], check=True)
    actual = json.loads((out / "models.json").read_text())
    expected = json.loads((HERE / "sample/models.json").read_text())
    if actual != expected:
        raise ValueError("generated metadata differs from the captured models")
    checks = []
    for model in actual["models"]:
        mesh = inspect(out / model["file"])
        if mesh["sha256"] != model["sha256"] or mesh["triangles"] != model["triangles"]:
            raise ValueError("generated STL differs from its receipt")
        checks.append(mesh)
    if args.viewer_bundle:
        bundle = args.viewer_bundle.resolve()
        if bundle.name != "viewer.bundle.js" or not bundle.is_file():
            parser.error("--viewer-bundle must name a built viewer.bundle.js")
        subprocess.run(
            [
                *host,
                "-mount",
                str(bundle.parent) + ":/viewer:ro",
                "-c",
                "python3 /recipe/view.py",
            ],
            check=True,
        )
    if args.cura:
        for model in actual["models"]:
            stl = out / model["file"]
            with stl.with_suffix(".slicer.log").open("w") as log:
                subprocess.run(
                    [
                        args.cura.resolve(),
                        "-v",
                        "-s",
                        "layerThickness=200",
                        "-s",
                        "initialLayerThickness=200",
                        "-s",
                        "extrusionWidth=400",
                        "-o",
                        stl.with_suffix(".gcode"),
                        stl,
                    ],
                    cwd=out,
                    stdout=log,
                    stderr=log,
                    check=True,
                    timeout=120,
                )
        subprocess.run([*host, "-c", "python3 /recipe/layers.py"], check=True)
        if json.loads((out / "layers.json").read_text()) != json.loads(
            (HERE / "sample/layers.json").read_text()
        ):
            raise ValueError("slice output differs from the pinned reader")
    (out / "checked.json").write_text(json.dumps(checks, indent=2) + "\n")
    print(json.dumps(checks, indent=2))


if __name__ == "__main__":
    main()
