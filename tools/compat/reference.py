"""Prevent accidental recapture from a candidate or unrecorded executable."""
import hashlib
import json
from pathlib import Path


def authenticate_capture(binary):
    baseline = json.loads((Path(__file__).resolve().parents[2] / "tests/contract/baseline.json").read_text())
    metadata = json.loads((binary.parent / "build.json").read_text())
    if metadata["source_sha"] != baseline["source_sha"] or metadata["version"] != baseline["version"]:
        raise ValueError("capture requires the recorded source revision and version")
    if not metadata.get("go_version", "").startswith("go version "):
        raise ValueError("capture requires a recorded Go reference build")
    if metadata["binary_sha256"] != hashlib.sha256(binary.read_bytes()).hexdigest():
        raise ValueError("capture binary differs from the recorded reference digest")
