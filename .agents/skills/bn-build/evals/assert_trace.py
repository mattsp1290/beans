#!/usr/bin/env python3
"""Check recorded subprocess observations, never fabricated desired-action logs."""
from __future__ import annotations
import argparse
import json
from pathlib import Path


def assert_trace(path: Path, root: Path) -> dict[str, int]:
    rows = [json.loads(line) for line in path.read_text().splitlines() if line.strip()]
    assert rows, "empty trace"
    ownership_observations = 0
    evidence_observations = 0
    for row in rows:
        assert isinstance(row["argv"], list) and row["argv"], "missing actual argv"
        assert type(row["returncode"]) is int, "missing actual outcome"
        assert Path(row["cwd"]).resolve().is_relative_to(root.resolve()), "working directory escaped fixture"
        assert row["elapsed"] >= 0
        argv = row["argv"]
        if argv[0] == "git" and "push" in argv:
            # Initial fixture setup pushes main once. Subsequent default branch
            # stability is proved separately by querying the bare remote SHA.
            assert not any(value.startswith(("https://", "ssh://", "git@")) for value in argv), "network remote in trace"
        if len(argv) > 2 and Path(argv[1]).name == "mvp_state.py":
            output = json.loads(row["stdout"])
            if row["returncode"]:
                assert output.get("error") and output.get("message"), "failure lacks actionable JSON"
            elif argv[2] in ("reserve-target", "check-owner"):
                assert output["owner_ref"].startswith("refs/heads/bn-mvp-owner/")
                assert output["milestone_ref"].startswith("refs/heads/bn-mvp-milestone/")
                assert len(output["owner_oid"]) in (40, 64)
                ownership_observations += 1
            elif argv[2] == "record":
                assert output["note"].startswith("bn-mvp:v1 ")
                assert output["published"] is False, "local cache cannot attest publication"
                assert json.loads(output["note"].split(" ", 1)[1]) == output["record"]
                evidence_observations += 1
    return {"commands": len(rows), "failures": sum(row["returncode"] != 0 for row in rows),
            "ownership_observations": ownership_observations, "evidence_observations": evidence_observations}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("trace", type=Path)
    parser.add_argument("--root", required=True, type=Path)
    args = parser.parse_args()
    print(json.dumps(assert_trace(args.trace, args.root)))
