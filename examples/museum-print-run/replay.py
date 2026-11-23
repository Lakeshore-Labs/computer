"""Build two museum proof sheets with stock Computer, then send the amendment to a local IPP emulator."""

import argparse
import base64
from hashlib import sha256
import json
import os
import plistlib
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time

HERE = Path(__file__).resolve().parent


def run(args, output, cwd=None, env=None):
    result = subprocess.run(
        [str(a) for a in args],
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=90,
    )
    if output:
        Path(output).write_bytes(result.stdout)
    if result.returncode:
        raise RuntimeError(
            f"{Path(args[0]).name} exited {result.returncode}: "
            + result.stdout.decode(errors="replace")[-1100:]
        )
    return result.stdout


def make_sheet(args, cards, folder, edition, slots):
    folder.mkdir()
    (cards / "order.json").write_text(
        json.dumps({"edition": edition, "slots": slots}) + "\n"
    )
    run(
        [
            args.computer,
            "-python",
            args.python_dir / "python.wasm",
            "-python-lib",
            args.python_dir / "lib",
            "-mount",
            str(HERE / "guest") + ":/recipe:ro",
            "-mount",
            str(cards) + ":/cards:ro",
            "-mount",
            str(folder) + ":/work",
            "-workdir",
            "/work",
            "-c",
            "python3 /recipe/impose.py",
        ],
        folder / "guest.txt",
    )
    ticket = json.loads((folder / "ticket.json").read_text())
    pdf = (folder / "sheet.pdf").read_bytes()
    assert (
        ticket["slots"] == slots
        and sha256(pdf).hexdigest() == ticket["pdf_sha256"]
        and len(pdf) == ticket["pdf_bytes"]
    )
    run(
        [
            "pdftoppm",
            "-scale-to",
            "1100",
            "-singlefile",
            "-png",
            folder / "sheet.pdf",
            folder / "sheet",
        ],
        None,
    )
    return ticket


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for key in ("computer", "python-dir", "output"):
        p.add_argument("--" + key, type=Path, required=True)
    p.add_argument("--cards", type=Path, default=HERE.parent / "postcard-press/sample")
    p.add_argument(
        "--capture",
        type=Path,
        help="optional Node script to record the emulator web page before shutdown",
    )
    args = p.parse_args()
    args.computer = args.computer.resolve()
    args.python_dir = args.python_dir.resolve()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        for name, f in [
            ("getty", "desktop-card1-getty.svg"),
            ("cleveland", "desktop-card2-cleveland.svg"),
        ]:
            run(
                [
                    "rsvg-convert",
                    "--format",
                    "pdf",
                    "--output",
                    work / (name + ".pdf"),
                    args.cards / f,
                ],
                None,
                env={**os.environ, "SOURCE_DATE_EPOCH": "1795305600"},
            )
        start = make_sheet(
            args, work, out / "first", "first", ["getty", "getty", "getty", "cleveland"]
        )
        revised = make_sheet(
            args,
            work,
            out / "amended",
            "amended",
            ["getty", "cleveland", "cleveland", "cleveland"],
        )
        if start["pdf_sha256"] == revised["pdf_sha256"]:
            raise RuntimeError("Unchanged proof")
        run(
            [sys.executable, HERE / "check/verify.py", work, out],
            out / "proof-check.txt",
        )
        spool = work / "spool"
        spool.mkdir()
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        uri = f"ipp://localhost:{port}/ipp/print"
        with (out / "printer.log").open("wb") as log:
            printer = subprocess.Popen(
                [
                    "ippeveprinter",
                    "-n",
                    "localhost",
                    "-r",
                    "off",
                    "-p",
                    str(port),
                    "-d",
                    str(spool),
                    "-k",
                    "-f",
                    "application/pdf",
                    "-s",
                    "60",
                    "--no-web-forms",
                    "Museum proof press",
                ],
                stdout=log,
                stderr=log,
            )
            try:
                for _ in range(100):
                    if printer.poll() is not None:
                        raise RuntimeError(
                            "Virtual printer stopped. Inspect printer.log."
                        )
                    try:
                        with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                            break
                    except OSError:
                        time.sleep(0.05)
                run(
                    [
                        "ipptool",
                        "-4",
                        "-T",
                        "10",
                        "-t",
                        "-f",
                        out / "amended/sheet.pdf",
                        uri,
                        HERE / "check/print.test",
                    ],
                    out / "submitted.txt",
                )
                query = run(
                    ["ipptool", "-4", "-X", uri, HERE / "check/inspect.test"],
                    out / "queue.plist",
                )
                info = plistlib.loads(query)
                matching = [
                    f
                    for f in spool.glob("*")
                    if f.is_file()
                    and sha256(f.read_bytes()).hexdigest() == revised["pdf_sha256"]
                ]
                jobs = [
                    v for v in info["Tests"][0]["ResponseAttributes"] if "job-id" in v
                ]
                assert len(matching) == 1 and info["Successful"]
                assert (
                    len(jobs) == 1
                    and jobs[0]["job-state"] == 9
                    and jobs[0]["job-name"] == "Amended-postcards"
                )
                if args.capture:
                    run(
                        ["node", args.capture, f"http://localhost:{port}/", out],
                        out / "browser-log.txt",
                    )
                receipt = {
                    "backend": "CUPS ippeveprinter",
                    "submitter": "CUPS ipptool",
                    "guest": "Computer WASI Python and pypdf",
                    "media": "A4 landscape; four A6 cards",
                    "first": start,
                    "amended": revised,
                    "submitted_editions": ["amended"],
                    "completed_jobs": jobs,
                    "spool_pdf_sha256": sha256(matching[0].read_bytes()).hexdigest(),
                    "physical_output": False,
                }
                (out / "receipt.json").write_text(
                    json.dumps(receipt, indent=2, default=str, ensure_ascii=False)
                    + "\n"
                )
            finally:
                printer.terminate()
                try:
                    printer.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    printer.kill()
                    printer.wait()
    page_data = {**receipt, "queue_capture": bool(args.capture)}
    page_data["pdfs"] = {
        edition: base64.b64encode((out / edition / "sheet.pdf").read_bytes()).decode(
            "ascii"
        )
        for edition in ("first", "amended")
    }
    template = (HERE / "desk.html").read_text()
    (out / "index.html").write_text(
        template.replace(
            "{{DATA}}",
            json.dumps(page_data, ensure_ascii=False).replace("<", r"\u003c"),
        )
    )
    print(out / "receipt.json")


if __name__ == "__main__":
    main()
