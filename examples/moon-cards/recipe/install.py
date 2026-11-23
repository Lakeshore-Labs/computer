"""Prepare verified pure-Python modules for a read-only WASI mount."""

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import tarfile
from urllib.request import urlopen


def prepare(cache, vendor):
    cache.mkdir(parents=True, exist_ok=True)
    vendor.mkdir(parents=True, exist_ok=True)
    pins = json.loads(Path(__file__).with_name("packages.json").read_text())
    for pin in pins:
        package = cache / pin["filename"]
        if not package.exists():
            with urlopen(pin["url"], timeout=30) as response:
                payload = response.read()
            if hashlib.sha256(payload).hexdigest() != pin["sha256"]:
                raise ValueError(pin["filename"] + " did not match its pinned digest")
            package.write_bytes(payload)
        if hashlib.sha256(package.read_bytes()).hexdigest() != pin["sha256"]:
            raise ValueError(pin["filename"] + " did not match its pinned digest")
        if package.suffix == ".whl":
            shutil.copyfile(package, vendor / package.name)
        else:
            # PyYAML publishes its portable Python implementation under lib/yaml in the source distribution.
            with tarfile.open(package, "r:gz") as archive:
                for member in archive:
                    path = PurePosixPath(member.name)
                    if (
                        member.isfile()
                        and len(path.parts) == 4
                        and path.parts[1:3] == ("lib", "yaml")
                        and path.suffix == ".py"
                    ):
                        (vendor / "yaml").mkdir(exist_ok=True)
                        (vendor / "yaml" / path.name).write_bytes(
                            archive.extractfile(member).read()
                        )
            if not (vendor / "yaml/__init__.py").is_file():
                raise ValueError(
                    "The pinned archive is missing the Python yaml package"
                )


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("cache", type=Path)
    ap.add_argument("vendor", type=Path)
    args = ap.parse_args()
    prepare(args.cache, args.vendor)
