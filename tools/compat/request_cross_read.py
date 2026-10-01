#!/usr/bin/env python3
"""Read actual Rust-produced request documents with the immutable Go codec."""
import json
import os
import subprocess

from request_reference import ROOT, capture


def main():
    work = ROOT / ".compat/request-cross-read"
    work.mkdir(parents=True, exist_ok=True)
    candidates = work / "rust-requests.json"
    candidates.unlink(missing_ok=True)
    subprocess.run(["cargo", "test", "--locked", "--test", "domain",
                    "request_codec::reader_and_all_mutations_match_fixed_go", "--", "--exact"],
                   cwd=ROOT, env=dict(os.environ, BN_RUST_REQUEST_OUTPUT=str(candidates)), check=True)
    corpus = json.loads((ROOT / "tests/contract/requests.json").read_text())
    if capture() != corpus:
        raise SystemExit("Request corpus changed during cross-reading")
    actual = capture(candidates)["cases"]
    (work / "go-reads.json").write_text(json.dumps(actual, indent=2, ensure_ascii=False) + "\n")
    for case in actual:
        if "error" in case:
            raise SystemExit(f"Go rejected actual Rust document {case['name']}: {case['error']}")
        for key in ("metadata", "body", "log"):
            if case[key] != case["expected_" + key]:
                raise SystemExit(f"Go reading Rust document {case['name']} differs: {key}")
        if case["encoded"] != case["input"]:
            raise SystemExit(f"Go rewrites Rust document {case['name']}")
    print(f"fixed Go accepted {len(actual)} actual Rust-produced request documents without rewriting")


if __name__ == "__main__":
    main()
