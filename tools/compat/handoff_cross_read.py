#!/usr/bin/env python3
"""Read actual Rust-produced handoff documents with the immutable Go codec."""
import json
import os
import subprocess

from handoff_reference import ROOT, capture


def main():
    work = ROOT / ".compat/handoff-cross-read"
    work.mkdir(parents=True, exist_ok=True)
    candidates = work / "rust-handoffs.json"
    candidates.unlink(missing_ok=True)
    subprocess.run(["cargo", "test", "--locked", "--test", "domain",
                    "handoffs::reader_and_all_mutations_match_fixed_go", "--", "--exact"],
                   cwd=ROOT, env=dict(os.environ, BN_RUST_HANDOFF_OUTPUT=str(candidates)), check=True)
    corpus = json.loads((ROOT / "tests/contract/handoffs.json").read_text())
    if capture() != corpus:
        raise SystemExit("Handoff corpus changed during cross-reading")
    actual = capture(candidates)["cases"]
    (work / "go-reads.json").write_text(json.dumps(actual, indent=2, ensure_ascii=False) + "\n")
    for case in actual:
        if "expected_error" in case:
            if case.get("error") != case["expected_error"]:
                raise SystemExit(f"Go rejection differs: {case['name']}")
            continue
        if "error" in case:
            raise SystemExit(f"Go rejected actual Rust document {case['name']}: {case['error']}")
        for key in ("metadata", "body"):
            if case[key] != case["expected_" + key]:
                raise SystemExit(f"Go reading Rust document {case['name']} differs: {key}")
        if case["encoded"] != case["input"]:
            raise SystemExit(f"Go rewrites Rust document {case['name']}")
    print(f"fixed Go read {len(actual)} actual Rust handoff documents: "
          f"{sum('error' not in case for case in actual)} accepted, "
          f"{sum('error' in case for case in actual)} matching boundary rejections")


if __name__ == "__main__":
    main()
