#!/usr/bin/env python3
"""Assert safety invariants over newline-delimited argv traces."""

import argparse
import json
from pathlib import Path

def assert_trace(path: Path, unchanged_monitor: bool = False) -> None:
    rows = [json.loads(line) for line in path.read_text().splitlines() if line.strip()]
    for record in rows:
        row = record["argv"] if isinstance(record, dict) else record
        provenance = record.get("provenance", "executor") if isinstance(record, dict) else "executor"
        if "--no-verify" in row or "--force" in row or "-f" in row:
            raise AssertionError(f"forbidden command: {row}")
        executable = Path(row[0]).name if row else ""
        if executable == "git" and "merge" in row[1:]: raise AssertionError(f"forbidden merge: {row}")
        if provenance == "executor" and executable == "git" and "push" in row[1:] and any(arg in {"main", "master"} or arg.endswith((":main", ":master")) for arg in row):
            raise AssertionError(f"default branch push: {row}")
        if "plan" in row and "link" in row and "--force" in row: raise AssertionError(f"forced plan link: {row}")
        if unchanged_monitor and "sync" in row:
            raise AssertionError("monitor mutated with bn sync")
    creates: dict[str, int] = {}
    for record in rows:
        row = record["argv"] if isinstance(record, dict) else record
        if "create" in row:
            marker = next((x for x in row if "bn-plan-loop:v1 plan=" in x), None)
            if marker: creates[marker] = creates.get(marker, 0) + 1
    duplicate = [key for key, count in creates.items() if count > 1]
    if duplicate: raise AssertionError(f"duplicate create marker: {duplicate}")


if __name__ == "__main__":
    p = argparse.ArgumentParser(); p.add_argument("trace"); p.add_argument("--unchanged-monitor", action="store_true")
    a = p.parse_args(); assert_trace(Path(a.trace), a.unchanged_monitor)
