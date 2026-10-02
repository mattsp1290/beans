#!/usr/bin/env python3
"""Capture raw diagnostic byte contracts from immutable Go."""
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


def capture():
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    harness = ROOT / "tools/compat/raw_diagnostics_census.go.txt"
    result = {"schema": "beans-raw-diagnostics-v1", "source_sha": baseline["source_sha"],
              "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="beans-raw-diagnostics-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "vault/migration_raw_diagnostics_test.go").write_bytes(harness.read_bytes())
        output = source / "raw-diagnostics.json"
        subprocess.run(["go", "test", "./vault", "-run", "^TestMigrationRawDiagnostics$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_RAW_DIAGNOSTICS_OUTPUT=str(output), GOTOOLCHAIN="go1.25.7"), check=True)
        result.update(json.loads(output.read_text()))
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/raw-diagnostics-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Raw diagnostics corpus differs from fixed Go")
        print("fixed Go raw diagnostics corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, separators=(",", ":"), ensure_ascii=False) + "\n")
        print(args.output)
