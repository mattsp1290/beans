#!/usr/bin/env python3
"""Capture canonical plan path/model and diagnostic contracts from immutable Go."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def compact(value):
    """Intern repeated long strings/byte arrays without changing any source byte."""
    blobs, indices = [], {}
    def pack(item):
        if ((isinstance(item, str) and len(item) > 64) or
                (isinstance(item, list) and len(item) > 64 and all(type(x) is int for x in item))):
            key = json.dumps(item, separators=(",", ":"), ensure_ascii=False)
            if key not in indices:
                indices[key] = len(blobs)
                blobs.append(item)
            return {"blob": indices[key]}
        if isinstance(item, list):
            return [pack(x) for x in item]
        if isinstance(item, dict):
            return {k: pack(x) for k, x in item.items()}
        return item
    result = pack(value)
    result["blobs"] = blobs
    return result


def capture():
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    harness = ROOT / "tools/compat/plan_bytes_census.go.txt"
    result = {"schema": "beans-plan-bytes-v1", "source_sha": baseline["source_sha"],
              "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="beans-plan-bytes-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "plan/migration_bytes_test.go").write_bytes(harness.read_bytes())
        output = source / "plan-bytes.json"
        subprocess.run(["go", "test", "./plan", "-run", "^TestMigrationPlanBytes$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_PLAN_BYTES_OUTPUT=str(output), GOTOOLCHAIN="go1.25.7"), check=True)
        result.update(json.loads(output.read_text()))
    return compact(result)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/plan-bytes-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Plan byte corpus differs from fixed Go")
        print("fixed Go plan byte corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, separators=(",", ":"), ensure_ascii=False) + "\n")
        print(args.output)
