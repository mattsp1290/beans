#!/usr/bin/env python3
"""Compare in-process Go/Rust loading on the same WP1 5,000-issue fixture."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile
from runner import environment, fixture

ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rust-bin", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    binary = args.rust_bin.resolve(strict=True)
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-index-benchmark-") as directory:
        root = Path(directory)
        fixture_root = root / "fixture"
        fixture_root.mkdir()
        env = environment(fixture_root)
        fixture(fixture_root, "seeded", env)
        issues = fixture_root / "hub/projects/alpha/issues"
        template = (issues / "alpha-a1b2-contract-issue.md").read_text()
        for i in range(4999):
            identity = "alpha-" + format(i, "08x")
            (issues / (identity + "-contract-issue.md")).write_text(template.replace("alpha-a1b2", identity))
        fixture_hash = hashlib.sha256()
        for path in sorted((fixture_root / "hub").rglob("*")):
            if path.is_file() and ".git" not in path.parts:
                fixture_hash.update(path.relative_to(fixture_root / "hub").as_posix().encode())
                fixture_hash.update(b"\0" + path.read_bytes() + b"\0")
        source = root / "go"
        source.mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "vault/migration_index_bench_test.go").write_bytes((ROOT / "tools/compat/index_bench.go.txt").read_bytes())
        output = root / "go.json"
        subprocess.run(["go", "test", "./vault", "-run", "^TestMigrationIndexBench$", "-count=1"], cwd=source,
                       env=dict(os.environ, GOTOOLCHAIN="go1.25.7", BN_INDEX_BENCH_HUB=str(fixture_root / "hub"), BN_INDEX_BENCH_OUTPUT=str(output)), check=True, timeout=180)
        go = json.loads(output.read_text())
        rust = json.loads(subprocess.check_output([str(binary), str(fixture_root / "hub")], cwd=ROOT, timeout=180))
        record = {"schema": "beans-index-performance-v1", "os": platform.system(), "arch": platform.machine(),
                  "source_sha": baseline["source_sha"], "go_toolchain": "go1.25.7", "rust_toolchain": "1.98.1",
                  "rust_profile": "release", "rust_binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                  "fixture_sha256": fixture_hash.hexdigest(), "measurement": "production index load, excluding CLI/startup/output", "order": ["go", "rust"],
                  "go": go, "rust": rust, "median_ratio": rust["median_ms"] / go["median_ms"]}
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(record, indent=2) + "\n")
        print(f"Go median {go['median_ms']:.2f}ms; Rust median {rust['median_ms']:.2f}ms; ratio {record['median_ratio']:.3f}")


if __name__ == "__main__":
    main()
