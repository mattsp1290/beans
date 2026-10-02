#!/usr/bin/env python3
"""Capture plan snapshot and Linux filesystem contracts from immutable Go."""
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


def capture(rust_input=None):
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    harness = ROOT / "tools/compat/plan_bundle_census.go.txt"
    result = {"schema": "beans-plan-bundle-v1", "source_sha": baseline["source_sha"],
              "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="beans-plan-bundle-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "plan/migration_bundle_test.go").write_bytes(harness.read_bytes())
        for test in (["CrossRead"] if rust_input else ["Census", "Filesystem"]):
            output = source / (test + ".json")
            subprocess.run(["go", "test", "./plan", "-run", "^TestMigrationPlanBundle" + test + "$", "-count=1"],
                           cwd=source, env=dict(os.environ, BN_PLAN_BUNDLE_OUTPUT=str(output),
                                            BN_PLAN_BUNDLE_INPUT=str(Path(rust_input).resolve()) if rust_input else ""),
                           check=True)
            result.update(json.loads(output.read_text()))
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/plan-bundle-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Plan bundle corpus differs from the fixed Go source")
        print("fixed Go plan bundle corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
