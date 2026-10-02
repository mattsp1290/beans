#!/usr/bin/env python3
"""Repeatable startup and 5,000-issue end-to-end index-load baseline."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import statistics
import subprocess
import tempfile
import time

from runner import environment, fixture


def measure(binary, args, root, env, samples, expected_issue_count=None):
    values = []
    for i in range(samples + 3):
        start = time.perf_counter()
        process = subprocess.run([str(binary), *args], env=env, cwd=root / "cwd", check=True,
                                 stdout=subprocess.PIPE if expected_issue_count is not None else subprocess.DEVNULL,
                                 stderr=subprocess.PIPE, timeout=30)
        elapsed = (time.perf_counter() - start) * 1000
        if expected_issue_count is not None and len(json.loads(process.stdout)) != expected_issue_count:
            raise ValueError("benchmark did not load every fixture issue")
        if i >= 3:
            values.append(elapsed)
    return {"samples_ms": values, "median_ms": statistics.median(values),
            "p95_ms": sorted(values)[min(len(values) - 1, int(len(values) * 0.95))],
            "min_ms": min(values), "max_ms": max(values)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--samples", type=int, default=20)
    args = parser.parse_args()
    if args.samples < 5:
        parser.error("at least five samples required")
    binary = args.binary.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="beans-benchmark-") as directory:
        root = Path(directory)
        env = environment(root)
        fixture(root, "seeded", env)
        issues = root / "hub/projects/alpha/issues"
        template = (issues / "alpha-a1b2-contract-issue.md").read_text()
        for i in range(4999):
            identity = "alpha-" + format(i, "08x")
            (issues / (identity + "-contract-issue.md")).write_text(template.replace("alpha-a1b2", identity))
        record = {"schema": "beans-performance-baseline-v1", "os": platform.system(),
                  "arch": platform.machine(), "warmup_runs": 3, "issue_count": 5000,
                  "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                  "startup": measure(binary, ["--version"], root, env, args.samples),
                  "full_index": measure(binary, ["list", "--json", "--limit", "0", "--no-fetch", "--project", "alpha"], root, env, args.samples, 5000)}
        args.output.write_text(json.dumps(record, indent=2) + "\n")
        print(f"Startup median {record['startup']['median_ms']:.2f}ms; full index median {record['full_index']['median_ms']:.2f}ms")


if __name__ == "__main__":
    main()
