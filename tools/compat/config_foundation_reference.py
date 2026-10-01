#!/usr/bin/env python3
"""Capture workflow, type and duration contracts from immutable Go."""
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
    harness = ROOT / "tools/compat/config_foundation_census.go.txt"
    result = {"schema": "beans-config-foundation-v1", "source_sha": baseline["source_sha"],
              "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="beans-config-foundation-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_config_foundation_test.go").write_bytes(harness.read_bytes())
        output = source / "config-foundation.json"
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationConfigFoundation$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_CONFIG_FOUNDATION_OUTPUT=str(output)), check=True)
        result.update(json.loads(output.read_text()))
    return result


def print_table(result):
    return ("// Generated from unicode.IsPrint in the immutable Go configuration oracle.\n"
            "// Unicode " + result["unicode_version"] + ". Regenerate with config_foundation_reference.py --write-print-table.\n"
            "pub(super) const RANGES: &[(u32, u32)] = &[\n" +
            "".join(f"    ({start:#x}, {end:#x}),\n" for start, end in result["print_ranges"]) + "];\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/config-foundation-reference.json")
    parser.add_argument("--check", type=Path)
    parser.add_argument("--write-print-table", action="store_true")
    args = parser.parse_args()
    result = capture()
    table = ROOT / "src/domain/go_print_ranges.rs"
    if args.write_print_table:
        table.write_text(print_table(result))
    if args.check:
        if table.read_text() != print_table(result):
            raise SystemExit("Production Unicode print table differs from fixed Go")
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Configuration foundation corpus differs from the fixed Go source")
        print("fixed Go configuration foundation corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
