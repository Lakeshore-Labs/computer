"""Interrupt a synthetic packing job in its own child process and resume it with Gemini."""

import argparse
import hashlib
import json
import os
import shlex
import signal
import subprocess
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
REFERENCE = "LF-042"
EXPECTED = "LF-042 IVORY"
FIRST = (
    "Pack the synthetic lost-property claim. First use bash to run this exact command on its own: "
    "bash /recipe/pack.sh . Wait for that tool result. Remember the pickup word IVORY in the conversation only. "
    "After the command finishes, answer packed. The packing script can safely run twice."
)
SECOND = (
    "Finish the same claim. Check /work/packed.txt. If it is absent run bash /recipe/pack.sh and check again. "
    "Write /work/notice.txt containing only the claim reference from packed.txt, one space and the pickup word in my first message. Then answer done."
)


def terminal_events(buf):
    events = []
    for line in buf.splitlines():
        try:
            item = json.loads(line)
        except ValueError:
            continue
        if isinstance(item, dict):
            events.extend(k for k in item if k in ("answer", "unanswered"))
    return events


def markers(state):
    return sum(
        p.read_bytes().count(b"google-gemini-v1")
        for p in state.rglob("*")
        if p.is_file()
    )


def digest(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("computer", "engine", "bundle", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--model", default="gemini-3.5-flash-lite")
    args = parser.parse_args()
    key = os.environ.get("GEMINI_API_KEY") or os.environ.get("GOOGLE_API_KEY")
    if not key:
        parser.error("set GEMINI_API_KEY or GOOGLE_API_KEY")
    binary, engine, bundle, out = (
        p.resolve() for p in (args.computer, args.engine, args.bundle, args.output)
    )
    receipt = {
        "model": args.model,
        "direct_endpoint": "https://generativelanguage.googleapis.com/v1beta/openai",
        "cli_sha256": digest(binary),
        "bundle_sha256": digest(bundle / "agent.js"),
        "engine_sha256": digest(engine),
        "events": [],
    }
    out.mkdir(parents=True, exist_ok=False)
    start = time.monotonic()

    def event(name, **observed):
        receipt["events"].append(
            {"step": name, "elapsed_s": round(time.monotonic() - start, 2), **observed}
        )
        target = out / "checks.json"
        stage = target.with_suffix(".tmp")
        stage.write_text(json.dumps(receipt, indent=2) + "\n")
        stage.replace(target)
        print(name, flush=True)

    with tempfile.TemporaryDirectory(prefix="computer-claim-") as temporary:
        scratch = Path(temporary)
        work = scratch / "work"
        work.mkdir()
        state = scratch / "state"
        env = {
            "PATH": os.environ["PATH"],
            "HOME": str(scratch),
            "COMPUTER_MODEL_KEY": key,
        }
        base = [
            str(binary),
            "-mount",
            str(work) + ":/work",
            "-mount",
            str(HERE / "recipe") + ":/recipe:ro",
            "-agent",
            str(bundle),
            "-agent-engine",
            str(engine),
            "-agent-state",
            str(state),
            "-model-url",
            receipt["direct_endpoint"],
            "-model",
            args.model,
            "-agent-max-turns",
            "9",
            "-model-max-tokens",
            "1000",
            "-timeout",
            "95s",
            "-c",
        ]
        first = subprocess.Popen(
            [*base, "agent --json " + shlex.quote(FIRST)],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
            text=True,
        )
        try:
            event("opened", child_process=1)
            cutoff = time.monotonic() + 65
            while (
                first.poll() is None
                and time.monotonic() < cutoff
                and not (work / "in-progress.txt").is_file()
            ):
                time.sleep(0.05)
            if (
                not (work / "in-progress.txt").is_file()
                or (work / "in-progress.txt").read_text().strip() != REFERENCE
                or (work / "packed.txt").exists()
            ):
                raise RuntimeError("the first process missed the in-tool checkpoint")
            before = markers(state)
            if before < 1:
                raise RuntimeError("no provider replay marker before the stop")
            event("packing", in_progress=REFERENCE, packed=False, replay_markers=before)
        finally:
            if first.poll() is None:
                first.kill()
            stdout, _ = first.communicate(timeout=10)
        events = terminal_events(stdout)
        if first.returncode != -signal.SIGKILL or events:
            raise RuntimeError("first process did not stop at the expected checkpoint")
        event("interrupted", exit=first.returncode, terminal_events=events)
        (work / "desk-open.txt").write_text("Desk open\n")
        event("reopened", child_process=2)
        result = subprocess.run(
            [*base, "agent -c --json " + shlex.quote(SECOND)],
            env=env,
            capture_output=True,
            text=True,
            timeout=130,
            check=False,
        )
        events = terminal_events(result.stdout)
        for name, expected in [("packed.txt", REFERENCE), ("notice.txt", EXPECTED)]:
            if (
                not (work / name).is_file()
                or (work / name).read_text().strip() != expected
            ):
                raise RuntimeError(
                    "the resumed process did not write "
                    + name
                    + " with the expected fixture text"
                )
        conversations = sum(p.is_dir() for p in state.iterdir())
        if result.returncode or events != ["answer"] or conversations != 1:
            raise RuntimeError("unexpected resumed conversation or terminal event")
        receipt["passed"] = True
        event(
            "finished",
            exit=result.returncode,
            terminal_events=events,
            conversations=conversations,
            replay_markers=markers(state),
            packed=REFERENCE,
            notice=EXPECTED,
        )


if __name__ == "__main__":
    main()
