#!/usr/bin/env python3
"""Run enduring Rust qualification and audit exact executable ledger selectors.

The expected-value corpora are immutable static oracles with recorded provenance;
this gate never builds, launches, or reads an executable reference client.
"""
import hashlib
import json
from pathlib import Path
import platform
import subprocess

ROOT = Path(__file__).resolve().parents[2]
LEDGER = ROOT / "tests/contract/regressions.json"


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
    ledger = json.loads(LEDGER.read_text())
    if len(ledger["entries"]) != 225:
        raise RuntimeError("baseline ledger must retain all 225 scenarios")
    identities = set()
    selectors = set()
    retired = []
    for row in ledger["entries"]:
        identity = (row["source"], row["test"])
        if identity in identities:
            raise RuntimeError(f"duplicate baseline scenario: {identity}")
        identities.add(identity)
        if row["status"] == "retired":
            if not row.get("retirement_rationale"):
                raise RuntimeError(f"unjustified retirement: {identity}")
            retired.append(identity)
            continue
        if row["status"] != "ported":
            raise RuntimeError(f"unqualified scenario: {identity}")
        source, selector = row["destination"].split("::", 1)
        if selector not in executables.get(source, set()):
            raise RuntimeError(f"no exact executable native test: {identity}: {row['destination']}")
        if not row.get("native_assertion"):
            raise RuntimeError(f"missing assertion disposition: {identity}")
        selectors.add(row["destination"])
    for path in (ROOT / "tests").rglob("*.rs"):
        source = path.read_text()
        if any(token in source for token in ["tools/compat", '"../../issue/testdata/', '.join("cmd/bn/testdata/', "BN_RUST_", "BN_PLAN_", "BN_CONFIG_CODEC_RUST_OUTPUT"]):
            raise RuntimeError(f"migration dependency in native test: {path}")
    output = ROOT / ".verification/native"
    output.mkdir(parents=True, exist_ok=True)
    report = {
        "schema": "beans-native-qualification-v1", "git_sha": command(["git", "rev-parse", "HEAD"], capture_output=True).stdout.strip(),
        "os": platform.system(), "arch": platform.machine(),
        "ledger_sha256": hashlib.sha256(LEDGER.read_bytes()).hexdigest(),
        "baseline_scenarios": len(identities), "ported": len(identities) - len(retired),
        "retired": retired, "exact_selectors": sorted(selectors),
        "native_tests": {path: len(tests) for path, tests in executables.items()},
        "scope": "Executed Rust tests plus exact selector/disposition audit; no executable Go oracle. Verus proof requires separate native Linux x86_64 jobs.",
    }
    (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"qualified {len(identities)} baseline dispositions across {len(selectors)} exact native selectors; {output / 'report.json'}")


if __name__ == "__main__":
    main()
