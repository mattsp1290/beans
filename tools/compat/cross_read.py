#!/usr/bin/env python3
"""Read actual Rust-generated issue bytes with the immutable Go reader."""
import json
import os
from pathlib import Path
import subprocess

from frontmatter_reference import ROOT, capture


def expected_read(case):
    result = {"name": case["name"]}
    if "read_error" in case:
        result["error"] = case["read_error"]
    else:
        for key in ("metadata", "description", "body", "log", "extra"):
            result[key] = case["read_" + key]
    return result


def main():
    work = ROOT / ".compat/cross-read"
    work.mkdir(parents=True, exist_ok=True)
    candidates, reads = work / "rust-issues.json", work / "go-reads.json"
    candidates.unlink(missing_ok=True)
    reads.unlink(missing_ok=True)
    env = dict(os.environ, BN_RUST_ISSUE_EXPORT=str(candidates))
    subprocess.run(["cargo", "test", "--locked", "--test", "domain",
                    "creation::new_issue_files_match_go_encoding_and_reader_semantics", "--", "--exact"],
                   cwd=ROOT, env=env, check=True)
    result = capture(candidates, reads)
    corpus = json.loads((ROOT / "tests/contract/frontmatter-primitives.json").read_text())
    if result != corpus:
        raise SystemExit("fixed Go domain corpus changed during cross-reading")
    expected = [expected_read(case) for case in corpus["new_issues"] if "encode_error" not in case]
    actual = json.loads(reads.read_text())
    if actual != expected:
        raise SystemExit(f"Go reading actual Rust issue bytes differs; inspect {reads}")
    accepted = sum("error" not in item for item in actual)
    print(f"fixed Go read {len(actual)} Rust-generated files: {accepted} accepted, "
          f"{len(actual) - accepted} reference-matched boundary rejections")


if __name__ == "__main__":
    main()
