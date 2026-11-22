"""Replay independent offline Computer recipes with read-only synthetic inputs."""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent


def replay(recipe, binary, runtime, root):
    name = recipe["id"]
    workspace = root / name
    workspace.mkdir()
    options = [
        str(binary),
        "-mount",
        str(workspace) + ":/work",
        "-mount",
        str(HERE / "fixtures") + ":/source:ro",
    ]
    if recipe["wasi_python"]:
        options.extend(
            [
                "-python",
                str(runtime / "python.wasm"),
                "-python-lib",
                str(runtime / "lib"),
            ]
        )
    script = (HERE / "scripts" / (name + ".sh")).read_text()
    command = [*options, "-c", script]
    try:
        result = subprocess.run(
            command, text=True, capture_output=True, timeout=recipe.get("timeout", 90)
        )
    except subprocess.TimeoutExpired as error:
        stdout = (error.stdout or b"").decode(errors="replace")
        stderr = (error.stderr or b"").decode(errors="replace")
        result = subprocess.CompletedProcess(
            command, 124, stdout, stderr + "\nRecipe timed out.\n"
        )
    missing = [
        item
        for item in recipe["expect"]
        if item.casefold() not in result.stdout.casefold()
    ]
    artifacts = {}
    for filename in recipe.get("files", []):
        path = workspace / filename
        if not path.is_file() or path.stat().st_size == 0:
            missing.append(filename)
        else:
            artifacts[filename] = {
                "bytes": path.stat().st_size,
                "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            }
    record = {
        "id": name,
        "exit": result.returncode,
        "missing": missing,
        "artifacts": artifacts,
        "passed": result.returncode == 0 and not missing,
    }
    (workspace / "stdout.txt").write_text(result.stdout)
    (workspace / "stderr.txt").write_text(result.stderr)
    print(("PASS" if record["passed"] else "FAIL"), name, flush=True)
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "recipes", nargs="*", help="one or more recipe names, core, python or all"
    )
    parser.add_argument("--computer", type=Path)
    parser.add_argument(
        "--python-wasi", type=Path, help="directory with python.wasm and lib"
    )
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    recipes = json.loads((HERE / "recipes.json").read_text())
    if not args.recipes:
        for recipe in recipes:
            profile = "python" if recipe["wasi_python"] else "core"
            print(f"{recipe['id']:22} {profile:7} {recipe['title']}")
        return
    if not args.output or not args.computer:
        parser.error("Supply --output and --computer to run recipes.")
    if len(args.recipes) == 1 and args.recipes[0] in {"core", "python", "all"}:
        group = args.recipes[0]
        selected = [
            r
            for r in recipes
            if group == "all" or r["wasi_python"] == (group == "python")
        ]
    else:
        by_name = {r["id"]: r for r in recipes}
        if any(name not in by_name for name in args.recipes):
            parser.error("Unknown recipe. Run without names to see the list.")
        selected = [by_name[name] for name in dict.fromkeys(args.recipes)]
    runtime = args.python_wasi.resolve() if args.python_wasi else None
    if any(r["wasi_python"] for r in selected) and not (
        runtime and (runtime / "python.wasm").is_file() and (runtime / "lib").is_dir()
    ):
        parser.error(
            "This selection requires the project WASI Python release. Supply --python-wasi."
        )
    binary = args.computer.resolve()
    if not binary.is_file():
        parser.error("The Computer binary was not found.")
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    records = []
    for recipe in selected:
        records.append(replay(recipe, binary, runtime, out))
        (out / "results.json").write_text(json.dumps(records, indent=2) + "\n")
    return int(not all(r["passed"] for r in records))


if __name__ == "__main__":
    sys.exit(main())
