#!/usr/bin/env python3
"""Install the fixed Verus Linux x86_64 archive into ignored development storage."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import shutil
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
PINS = Path(__file__).with_name("pins.json")


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def install(output):
    pins = json.loads(PINS.read_text())
    if (platform.system(), platform.machine()) != (pins["platform"]["os"], pins["platform"]["arch"]):
        raise RuntimeError("pinned prebuilt Verus requires Linux x86_64; use its native CI runner")
    output.mkdir(parents=True, exist_ok=True)
    archive = output / pins["asset"]
    if not archive.exists():
        url = f"https://github.com/verus-lang/verus/releases/download/{pins['release']}/{pins['asset']}"
        partial = archive.with_suffix(".partial")
        with urllib.request.urlopen(url, timeout=60) as response, partial.open("wb") as target:
            shutil.copyfileobj(response, target)
        partial.replace(archive)
    if digest(archive) != pins["archive_sha256"]:
        raise RuntimeError("Verus archive checksum mismatch")
    with zipfile.ZipFile(archive) as bundle:
        for entry in bundle.infolist():
            path = Path(entry.filename)
            if path.is_absolute() or ".." in path.parts or path.parts[0] != pins["directory"]:
                raise RuntimeError("unexpected Verus archive path")
            target = output / path
            if entry.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            with bundle.open(entry) as source, target.open("wb") as destination:
                shutil.copyfileobj(source, destination)
            target.chmod((entry.external_attr >> 16) & 0o777)
    tools = output / pins["directory"]
    for name, expected in pins["binaries"].items():
        if digest(tools / name) != expected:
            raise RuntimeError("Verus tool checksum mismatch: " + name)
    return tools


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".verification/verus")
    args = parser.parse_args()
    print(install(args.output.resolve()))
