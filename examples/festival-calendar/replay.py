"""Publish a festival program from three independently started Computer MCP clients."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory

RECIPE = Path(__file__).resolve().parent


def run_client(command, receipt):
    result = subprocess.run(command, check=True, text=True, stdout=subprocess.PIPE, timeout=150)
    data = json.loads(result.stdout)
    receipt.write_text(json.dumps(data, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--computer", type=Path, required=True)
    parser.add_argument("--python-wasi", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    binary = str(args.computer.resolve())
    runtime = str(args.python_wasi.resolve())
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    params = [binary, str(out), str(RECIPE), runtime]
    with TemporaryDirectory() as temp:
        classpath = Path(temp) / "jars.txt"
        subprocess.run(["mvn", "-q", "-f", str(RECIPE / "pom.xml"),
                        "org.apache.maven.plugins:maven-dependency-plugin:3.7.0:build-classpath",
                        "-Dmdep.outputFile=" + str(classpath)], check=True, timeout=240)
        run_client(["node", str(RECIPE / "boxoffice.cjs"), *params], out / "boxoffice.json")
        run_client(["java", "-cp", classpath.read_text().strip(), str(RECIPE / "Projectionist.java"), *params],
                   out / "projectionist.json")
    run_client([sys.executable, str(RECIPE / "subscriber.py"), binary, str(out)], out / "subscriber.json")
    print((out / "subscriber.json").read_text())


if __name__ == "__main__":
    main()
