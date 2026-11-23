"""Ask a local Ollama model to look up a pickup ribbon, then print its label in a new Computer process."""

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import time
from urllib.parse import urlparse
from urllib.request import build_opener, ProxyHandler

JOBS = {"north": ("NORTH-17", "FERN"), "east": ("EAST-42", "COPPER")}
FIRST = "Use the read tool to read /orders/today.txt. Reply with the exact pickup ribbon and shelf from the file."
SECOND = (
    "Using the shelf and pickup ribbon we just read, use the write tool to make /desk/label.txt. "
    "Write the shelf, a single space, then the ribbon, with no other text. Report when saved."
)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def turn(cmd, env, prompt, resume):
    before = time.monotonic()
    mode = "agent -c --json " if resume else "agent --json "
    result = subprocess.run(
        [*cmd, "-c", mode + shlex.quote(prompt)],
        env=env,
        text=True,
        capture_output=True,
        timeout=180,
    )
    tool_calls, tool_results, answers = [], [], []
    for line in result.stdout.splitlines():
        packet = json.loads(line)
        if "answer" in packet:
            answers.append(packet["answer"])
        for entry in packet.get("entries", []):
            if entry["kind"] == "pi.assistant":
                for message in entry["model"]:
                    for item in message.get("content", []):
                        if isinstance(item, dict) and item.get("type") == "toolCall":
                            tool_calls.append(
                                {"name": item["name"], "arguments": item["arguments"]}
                            )
            if entry["kind"] == "pi.tool-result":
                for message in entry["model"]:
                    tool_results.append(
                        {
                            "name": message.get("toolName"),
                            "is_error": message.get("isError"),
                        }
                    )
    return {
        "exit": result.returncode,
        "seconds": round(time.monotonic() - before, 2),
        "prompt": prompt,
        "tool_calls": tool_calls,
        "tool_results": tool_results,
        "answers": answers,
    }


def main(args):
    address = urlparse(args.endpoint)
    if (
        address.scheme != "http"
        or address.hostname not in ("127.0.0.1", "localhost", "::1")
        or address.username
        or address.password
    ):
        raise ValueError("Supply a local Ollama HTTP endpoint.")
    url = args.endpoint.rstrip("/")
    local = build_opener(ProxyHandler({}))
    with local.open(url + "/api/tags", timeout=4) as response:
        installed = next(
            (m for m in json.load(response)["models"] if m["name"] == args.model), None
        )
    if installed is None:
        raise ValueError("Start Ollama with the selected model already installed.")
    with local.open(url + "/api/version", timeout=4) as response:
        version = json.load(response)["version"]
    args.out.mkdir(parents=True, exist_ok=False)
    desk = args.out.resolve() / "desk"
    desk.mkdir()
    shelf, ribbon = JOBS[args.job]
    orders = args.out.resolve() / "orders"
    orders.mkdir()
    source = orders / "today.txt"
    source.write_text(f"Wardrobe B\nShelf: {shelf}\nPickup ribbon: {ribbon}\n")
    with tempfile.TemporaryDirectory() as home:
        state = Path(home) / "state"
        env = {"HOME": home, "PATH": os.defpath, "COMPUTER_MODEL_KEY": "local-desk"}
        cmd = [
            str(args.computer.resolve()),
            "-mount",
            str(desk) + ":/desk",
            "-mount",
            str(orders) + ":/orders:ro",
            "-workdir",
            "/desk",
            "-agent",
            str(args.bundle.resolve()),
            "-agent-engine",
            str(args.engine.resolve()),
            "-agent-state",
            str(state),
            "-model-url",
            url + "/v1",
            "-model",
            args.model,
            "-agent-max-turns",
            "4",
            "-model-max-tokens",
            "1400",
            "-model-context",
            "8192",
        ]
        started = dt.datetime.now(dt.timezone.utc).isoformat()
        lookup = turn(cmd, env, FIRST, False)
        print_label = turn(cmd, env, SECOND, True)
    destination = desk / "label.txt"
    label = destination.read_text() if destination.exists() else ""

    def completed(run, tool):
        return run["exit"] == 0 and any(
            t["name"] == tool and t["is_error"] is False for t in run["tool_results"]
        )

    checks = {
        "read_returned": completed(lookup, "read"),
        "answer_has_shelf_and_ribbon": any(
            shelf in str(answer) and ribbon in str(answer)
            for answer in lookup["answers"]
        ),
        "write_returned": completed(print_label, "write"),
        "label_matches": label.strip() == f"{shelf} {ribbon}",
    }
    host_command = [
        str(args.computer.resolve()),
        "-mount",
        str(desk) + ":/desk",
        "-mount",
        str(orders) + ":/orders:ro",
        "-mount",
        str(Path(__file__).resolve().parent) + ":/recipe:ro",
        "-python",
        str(args.python_dir.resolve() / "python.wasm"),
        "-python-lib",
        str(args.python_dir.resolve() / "lib"),
        "-workdir",
        "/desk",
    ]
    with tempfile.TemporaryDirectory() as empty_home:
        for mode in ("inspect", "correct"):
            subprocess.run(
                [*host_command, "-c", "python3 /recipe/print_label.py " + mode],
                check=True,
                capture_output=True,
                timeout=40,
                env={"HOME": empty_home, "PATH": os.defpath},
            )
            gate = json.loads((desk / (mode + ".json")).read_text())
            if mode == "inspect":
                initial = gate
                initial_disk_queue = (desk / "ticket.zpl").is_file()
            if gate["queue_file_exists"]:
                break
    ticket = (desk / "ticket.zpl").read_text()
    checks["queue_decision_matches_draft"] = (
        initial["decision"] == "accepted"
    ) == checks["label_matches"]
    checks["first_queue_matches_observation"] = (
        initial_disk_queue == initial["queue_file_exists"] == checks["label_matches"]
    )
    checks["draft_preserved"] = (
        destination.is_file() and destination.read_text() == label
    )
    checks["printer_job_matches_source"] = (
        ticket.splitlines()[0] == "^XA"
        and ticket.splitlines()[-1] == "^XZ"
        and [
            line.split("^FD", 1)[1].removesuffix("^FS")
            for line in ticket.splitlines()
            if "^FD" in line
        ]
        == ["WARDROBE PICKUP", shelf + " " + ribbon]
        and gate["zpl_sha256"] == sha((desk / "ticket.zpl").read_bytes())
    )
    receipt = {
        "job": args.job,
        "started_utc": started,
        "model": args.model,
        "model_digest": installed["digest"],
        "model_details": {
            key: installed.get("details", {}).get(key)
            for key in ("family", "parameter_size", "quantization_level")
        },
        "ollama_version": version,
        "source_sha256": sha(source.read_bytes()),
        "bundle_sha256": sha((args.bundle / "agent.js").read_bytes()),
        "engine_sha256": sha(args.engine.read_bytes()),
        "lookup": lookup,
        "continued": print_label,
        "label": label,
        "checks": checks,
        "initial_gate": initial,
        "final_gate": gate,
        "passed": all(v for key, v in checks.items() if key != "label_matches"),
    }
    (args.out / "receipt.json").write_text(
        json.dumps(receipt, ensure_ascii=False, indent=2) + "\n"
    )
    print(json.dumps(checks), flush=True)
    return 0 if receipt["passed"] else 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for option in ("computer", "bundle", "engine", "python-dir", "out"):
        parser.add_argument("--" + option, type=Path, required=True)
    parser.add_argument("--endpoint", default="http://127.0.0.1:11434")
    parser.add_argument("--model", default="qwen3:0.6b")
    parser.add_argument("--job", choices=JOBS, required=True)
    raise SystemExit(main(parser.parse_args()))
