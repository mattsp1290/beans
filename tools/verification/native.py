#!/usr/bin/env python3
"""Execute permanent native Rust tests and record their compiled suite inventory.

The expected-value corpora are immutable static oracles with recorded provenance;
this gate never builds, launches, or reads an executable reference client.
"""
import json
from pathlib import Path
import platform
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def command(argv, **kwargs):
    return subprocess.run(argv, cwd=ROOT, check=True, text=True, **kwargs)


def main():
    command(["cargo", "test", "--locked", "--workspace"])
    build = command(["cargo", "test", "--locked", "--workspace", "--no-run", "--message-format=json"], capture_output=True)
    executables = {}
    for line in build.stdout.splitlines():
        record = json.loads(line)
        if record.get("reason") == "compiler-artifact" and record.get("executable") and record["profile"]["test"]:
            source = str(Path(record["target"]["src_path"]).relative_to(ROOT))
            tests = command([record["executable"], "--list"], capture_output=True).stdout
            executables[source] = {line.removesuffix(": test") for line in tests.splitlines() if line.endswith(": test")}
    output = ROOT / ".verification/native"
    output.mkdir(parents=True, exist_ok=True)
    report = {
        "schema": "beans-native-verification-v1", "git_sha": command(["git", "rev-parse", "HEAD"], capture_output=True).stdout.strip(),
        "os": platform.system(), "arch": platform.machine(),
        "native_tests": {path: len(tests) for path, tests in executables.items()},
        "scope": "Executed permanent Rust tests. Verus proof requires separate native Linux x86_64 jobs.",
    }
    (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"executed {sum(map(len, executables.values()))} native tests; {output / 'report.json'}")


if __name__ == "__main__":
    main()
