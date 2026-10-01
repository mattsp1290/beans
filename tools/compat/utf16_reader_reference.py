#!/usr/bin/env python3
"""Capture UTF16 YAML reader contracts from immutable Go."""
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


def pack_bytes(value):
    """Compress repeated UTF-16 units without losing any captured bytes."""
    chunks = []
    literal = []
    at = 0
    while at < len(value):
        pattern = value[at:at + 2]
        count = 1
        while len(pattern) == 2 and value[at + count * 2:at + (count + 1) * 2] == pattern:
            count += 1
        if count >= 4:
            if literal:
                chunks.append(literal)
                literal = []
            chunks.append({"pattern": pattern, "count": count})
            at += count * 2
        else:
            literal.append(value[at])
            at += 1
    if literal:
        chunks.append(literal)
    return chunks


def compact(value):
    if isinstance(value, dict):
        return {key: pack_bytes(child) if key == "bytes" else compact(child)
                for key, child in value.items()}
    if isinstance(value, list):
        return [compact(child) for child in value]
    return value


def capture():
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    harness = ROOT / "tools/compat/utf16_reader_census.go.txt"
    result = {"schema": "beans-utf16-reader-v1", "source_sha": baseline["source_sha"],
              "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="beans-utf16-reader-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_utf16_reader_test.go").write_bytes(harness.read_bytes())
        output = source / "utf16-reader.json"
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationUTF16Reader$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_UTF16_READER_OUTPUT=str(output), GOTOOLCHAIN="go1.25.7"), check=True)
        result.update(compact(json.loads(output.read_text())))
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/utf16-reader-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("UTF16 reader corpus differs from fixed Go")
        print("fixed Go UTF16 reader corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, separators=(",", ":"), ensure_ascii=False) + "\n")
        print(args.output)
