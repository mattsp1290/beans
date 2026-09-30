#!/usr/bin/env python3
"""Capture ID grammar, filename and Unicode slug rules from the fixed Go tree."""
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
    harness = ROOT / "tools/compat/id_census.go.txt"
    with tempfile.TemporaryDirectory(prefix="beans-id-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_id_test.go").write_bytes(harness.read_bytes())
        output = source / "ids.json"
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationIDCensus$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_ID_OUTPUT=str(output)),
                       check=True, capture_output=True)
        return {"schema": "beans-ids-v1", "source_sha": baseline["source_sha"],
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                **json.loads(output.read_text())}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/id-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("ID corpus differs from the fixed Go source")
        print("fixed Go ID, filename and Unicode slug corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
