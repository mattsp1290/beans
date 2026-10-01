#!/usr/bin/env python3
"""Capture remote URL contracts from immutable Go."""
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
    harness = ROOT / "tools/compat/remote_census.go.txt"
    result = {"schema": "beans-remote-v1", "source_sha": baseline["source_sha"],
              "go_toolchain": "go1.25.7", "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="beans-remote-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "vault/migration_remote_test.go").write_bytes(harness.read_bytes())
        output = source / "remote.json"
        subprocess.run(["go", "test", "./vault", "-run", "^TestMigrationRemote$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_REMOTE_OUTPUT=str(output), GOTOOLCHAIN="go1.25.7"), check=True)
        result.update(json.loads(output.read_text()))
    return result


def lower_table(result):
    return "// Generated from immutable Go unicode.ToLower (Unicode " + result["unicode_version"] + ").\n" + "pub(super) const LOWER: &[(u32, u32)] = &[\n" + "".join(f"    ({a:#x}, {b:#x}),\n" for a,b in result["lowercase"]) + "];\n"

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/remote-reference.json")
    parser.add_argument("--check", type=Path)
    parser.add_argument("--write-lower-table", action="store_true")
    args = parser.parse_args()
    result = capture()
    table = ROOT / "src/vault/go_lower_table.rs"
    if args.write_lower_table:
        table.write_text(lower_table(result))
    if args.check:
        if table.read_text() != lower_table(result):
            raise SystemExit("Go lower table differs")
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Remote corpus differs from fixed Go")
        print("fixed Go remote corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
