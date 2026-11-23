"""Back up and restore two Computer-generated library editions through restic and rclone."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--computer", type=Path, required=True)
parser.add_argument("--python-dir", type=Path, required=True)
parser.add_argument("--restic", default="restic")
parser.add_argument("--rclone", default="rclone")
parser.add_argument("--output", type=Path, required=True)
args = parser.parse_args()


def executable(name):
    found = shutil.which(str(name))
    if not found:
        parser.error(f"Cannot find executable {name}")
    return str(Path(found).resolve())


restic, rclone, computer = map(executable, (args.restic, args.rclone, args.computer))
runtime = args.python_dir.resolve()
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=False)
report = output / "report"
report.mkdir()


def hashes(folder):
    return {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(folder.iterdir())}


with tempfile.TemporaryDirectory(prefix="reading-room-") as scratch:
    work = Path(scratch)
    for name in ("home", "library", "metadata"):
        (work / name).mkdir()
    password = secrets.token_urlsafe(32)
    config = work / "rclone.conf"
    config.write_text("[shelf]\ntype = local\n")
    env = {"HOME": str(work / "home"), "RCLONE_CONFIG": str(config), "PATH": os.defpath,
           "TZ": "UTC", "LANG": "en_US.UTF-8"}
    history = []

    def run(label, cmd, *, archive=False, expected=0):
        child_env = dict(env)
        if archive:
            child_env["RESTIC_PASSWORD"] = password
        res = subprocess.run(cmd, env=child_env, cwd=work, capture_output=True, text=True, timeout=180)
        history.append({"step": label, "exit": res.returncode})
        if res.returncode != expected:
            raise RuntimeError(f"{label} returned {res.returncode}: {res.stderr[-700:]}")
        return res.stdout

    repo = [restic, "-r", f"rclone:shelf:{work}/repo", "--cache-dir", str(work / "cache"),
            "-o", f"rclone.program={rclone}"]

    def backup(label, *args):
        return run(label, repo + list(args), archive=True)

    def guest(label, mounts, script):
        cmd = [computer, "-python", str(runtime / "python.wasm"), "-python-lib", str(runtime / "lib")]
        for guest_name, directory, readonly in [("/recipe", here / "guest", True), *mounts]:
            cmd.extend(["-mount", f"{directory}:{guest_name}" + (":ro" if readonly else "")])
        return run(label, cmd + ["-c", script])

    version = run("restic version", [restic, "version"]).strip()
    rclone_version = run("rclone version", [rclone, "version"]).splitlines()[0]
    library = work / "library"
    backup("Initialize repository", "init")
    originals = {}
    for number, edition in enumerate(("original", "changed"), start=1):
        guest(f"Write {edition} edition", [("/library", library, False)],
              f"python3 /recipe/make_edition.py {number} /library")
        originals[edition] = hashes(library)
        backup(f"Back up {edition} edition", "backup", "--tag", edition, "--host", "reading-room",
               "--time", f"2026-11-22 09:{'00' if number == 1 else '30'}:00", "library")
    snapshots = json.loads(backup("List snapshots", "snapshots", "--json"))
    ids = {s["tags"][0]: s["id"] for s in snapshots}
    assert set(ids) == set(originals) and len(snapshots) == 2
    for edition, digest in ids.items():
        folder = output / "restored" / edition
        backup(f"Restore {edition} edition", "restore", digest + ":/library", "--target", str(folder))
        assert hashes(folder) == originals[edition]
    diff = backup("Compare snapshots", "diff", "--json", ids["original"], ids["changed"])
    changes = [json.loads(line) for line in diff.splitlines()]
    changed_paths = [Path(row["path"]).name for row in changes if row.get("message_type") == "change"]
    assert changed_paths == ["hours.csv"], changed_paths
    checked = backup("Read and check all repository data", "check", "--read-data")
    assert "no errors were found" in checked
    (work / "metadata" / "manifest.json").write_text(json.dumps(originals))
    guest("Compare both restored trees", [("/orig", output / "restored/original", True),
          ("/chg", output / "restored/changed", True), ("/meta", work / "metadata", True), ("/out", report, False)],
          "python3 /recipe/read_editions.py /orig /chg /meta /out")
    comparison = json.loads((report / "comparison.json").read_text())
    assert comparison["sha256"] == originals
    assert comparison["weekly_hours"] == {"original": 47.0, "changed": 56.0}
    assert comparison["changed_days"] == ["Friday", "Saturday", "Sunday"]
    assert comparison["titles"] == 24 and comparison["copies"] == 67
    assert all(all(matches.values()) for matches in comparison["verified"].values())
    receipt = {"restic": version, "rclone": rclone_version, "snapshot_ids": ids, "changed_files": changed_paths,
               "full_data_check": True, "host_and_guest_hashes_agree": True, "steps": history}
    (output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    assert all(password.encode() not in p.read_bytes() for p in output.rglob("*") if p.is_file())
    print(f"Restored {len(ids)} editions. The library gained nine weekly opening hours.")
