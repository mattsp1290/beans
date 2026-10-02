#!/usr/bin/env python3
"""Capture Linux closed-pipe and ENOSPC stdout behavior with real descriptors."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

from reference import authenticate_capture
from runner import CONTRACT, encode, environment, fixture, git_state, snapshot


def cases():
    return [{"id": f"stdout:{fault}:{name}", "fault": fault, "argv": argv}
            for fault in ("closed-pipe", "full-device")
            for name, argv in (("prime", ["prime"]), ("help", ["--help"]),
                               ("version", ["--version"]),
                               ("list-json", ["list", "--project", "alpha", "--json", "--no-fetch"]))]


def execute(binary, case):
    with tempfile.TemporaryDirectory(prefix="beans-stdout-") as directory:
        root = Path(directory)
        env = environment(root)
        fixture(root, "seeded", env)
        before = snapshot(root)
        if case["fault"] == "closed-pipe":
            reader, writer = os.pipe()
            os.close(reader)
            # No reader exists before the child writes: deterministic EPIPE,
            # independent of output size, consumer speed or scheduler timing.
            sink = os.fdopen(writer, "wb")
        elif case["fault"] == "full-device":
            sink = open("/dev/full", "wb", buffering=0)
        else:
            raise ValueError("unknown stdout fault")
        with sink:
            process = subprocess.run([str(binary), *case["argv"]], cwd=root / "cwd", env=env,
                                     stdin=subprocess.DEVNULL, stdout=sink, stderr=subprocess.PIPE,
                                     timeout=10)
        after = snapshot(root)
        return {"exit": process.returncode, "stderr_b64": encode(process.stderr.replace(
                    os.fsencode(root), b"${FIXTURE_ROOT}")),
                "changed_files": {k: v for k, v in after.items() if before.get(k) != v},
                "removed_files": sorted(set(before) - set(after)), "git": git_state(root, env)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "io-failures.json")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if args.mode == "capture":
        authenticate_capture(binary)
        corpus = {"schema": "beans-linux-stdout-failure-v1", "cases": cases()}
    else:
        corpus = json.loads(args.corpus.read_text())
    failures = []
    for case in corpus["cases"]:
        result = execute(binary, case)
        if args.mode == "capture":
            case["expected"] = result
        elif result != case["expected"]:
            failures.append(case["id"])
    if args.mode == "capture":
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print(f"Captured {len(corpus['cases'])} stdout failure cases")
    elif failures:
        parser.exit(1, "Stdout failure mismatches:\n" + "\n".join(failures) + "\n")
    else:
        print(f"Passed {len(corpus['cases'])} stdout failure cases")


if __name__ == "__main__":
    main()
