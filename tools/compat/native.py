#!/usr/bin/env python3
"""Record native Go baseline tests and retained executable help on CI runners."""
import argparse
import io
import json
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile

from build_reference import build
from runner import CONTRACT, encode, environment, fixture


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(".compat/native"))
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    build(output / "reference")
    binary = output / "reference" / "bn-go"
    baseline = json.loads((CONTRACT / "baseline.json").read_text())
    census = json.loads((output / "reference/commands.json").read_text())
    failures = []
    if census != json.loads((CONTRACT / "commands.json").read_text()):
        failures.append("native command census differs from baseline")
    root = CONTRACT.parents[1]
    archive = subprocess.run(["git", "archive", baseline["source_sha"]], cwd=root,
                             capture_output=True, check=True).stdout
    with tempfile.TemporaryDirectory(prefix="beans-native-tests-") as directory:
        source = Path(directory)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        test = subprocess.run(["go", "test", "./...", "-count=1"], cwd=source,
                              capture_output=True, timeout=600)
        (output / "go-test.log").write_bytes(test.stdout + test.stderr)
        if test.returncode != 0:
            failures.append("native Go baseline regression suite failed")
        print(test.stdout.decode(errors="replace"), end="")
        print(test.stderr.decode(errors="replace"), end="")
    probes = []
    with tempfile.TemporaryDirectory(prefix="beans-native-cli-") as directory:
        fixture_root = Path(directory)
        env = environment(fixture_root)
        fixture(fixture_root, "missing-hub", env)
        for command in census["commands"]:
            if command["retired"]:
                continue
            argv = command["path"] + ["--help"]
            process = subprocess.run([str(binary), *argv], cwd=fixture_root / "cwd", env=env,
                                     capture_output=True, timeout=15)
            probes.append({"argv": argv, "exit": process.returncode,
                           "stdout_b64": encode(process.stdout), "stderr_b64": encode(process.stderr)})
            if process.returncode != 0:
                failures.append("native help failed: " + " ".join(argv))
        version = subprocess.run([str(binary), "--version"], env=env, cwd=fixture_root / "cwd",
                                 capture_output=True, timeout=15)
        if version.stdout != b"bn version migration-oracle\n" or version.returncode != 0:
            failures.append("native version linking failed")
    report = {"schema": "beans-native-go-baseline-v1", "source_sha": baseline["source_sha"],
              "os": platform.system(), "arch": platform.machine(), "go_test_exit": test.returncode,
              "help_probes": probes, "failures": failures,
              "remaining": ["native Rust equivalence", "native mixed-client locking/recovery", "full native executable journeys"]}
    (output / "validation.json").write_text(json.dumps(report, indent=2) + "\n")
    if failures:
        parser.exit(1, "Native baseline failures: " + "; ".join(failures) + "\n")
    print(f"Native baseline passed: {report['os']} {report['arch']}, Go suite and {len(probes)} retained help probes")


if __name__ == "__main__":
    main()
