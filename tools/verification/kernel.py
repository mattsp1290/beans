#!/usr/bin/env python3
"""Verify shipped helper bodies and demonstrate rejection of weakened guards."""
import json
import os
import re
import subprocess

from install import PINS, ROOT, digest, install


def main():
    pins = json.loads(PINS.read_text())
    output = ROOT / ".compat/verification"
    output.mkdir(parents=True, exist_ok=True)
    tools = install(ROOT / ".compat/verus")
    env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
               RUSTUP_TOOLCHAIN=pins["rust"], VERUS_Z3_PATH=str(tools / "z3"),
               CARGO_TARGET_DIR=str(output / "kernel-target"))
    sources = ROOT / "crates/beans-kernel/src"
    observations = {}
    argv = ["cargo", "verus", "verify", "-p", "beans-kernel", "--locked"]

    def verify(name):
        result = subprocess.run(argv, cwd=ROOT, env=env, capture_output=True, timeout=360)
        text = (result.stdout + result.stderr).decode(errors="replace")
        log = "kernel-" + name + ".log"
        (output / log).write_text(text)
        results = [
            {"verified": int(verified), "errors": int(errors)}
            for verified, errors in re.findall(
                r"verification results::\s*(\d+) verified, (\d+) errors", text
            )
        ]
        observations[name] = {"argv": argv, "exit": result.returncode,
                              "log": log, "verification_results": results}
        print("kernel " + name + ": exit " + str(result.returncode), flush=True)
        return result, text

    originals = {path: path.read_bytes() for path in sorted(sources.glob("*.rs"))}
    try:
        for path, source in originals.items():
            if re.search(rb"\b(?:assume|admit)\s*\(|assume_specification|"
                         rb"verifier::(?:external(?:_body)?|axiom)", source):
                raise RuntimeError("unapproved trusted proof boundary in " + str(path))
        result, text = verify("proof")
        if result.returncode != 0:
            raise RuntimeError("mandatory kernel proof failed:\n" + text)
        results = observations["proof"]["verification_results"]
        # The final summary belongs to beans-kernel. vstd's separate 2059
        # obligations cannot stand in for the eight mandatory functions/proof.
        if not results or results[-1]["verified"] < 8 or results[-1]["errors"] != 0:
            raise RuntimeError("verifier did not report checked obligations:\n" + text)

        retry = sources / "retry.rs"
        mutations = [
            ("unowned-operation", retry, b"        && facts.owned_by_run\n    {", b"\n    {"),
            ("stale-head", retry,
             b"} else if facts.operation_present\n        && facts.head_is_operation\n",
             b"} else if facts.operation_present\n"),
            ("multiple-local-commits", retry,
             b"        && facts.local_commits == 1\n        && facts.owned_by_run\n    {",
             b"        && facts.owned_by_run\n    {"),
            ("missing-operation", retry, b"} else if facts.operation_present\n", b"} else if true\n"),
            ("fourth-push", retry, b"if facts.push_attempts >= 3 {", b"if facts.push_attempts > 3 {"),
            ("unconsumed-budget", retry, b"budget.remaining -= 1;", b"budget.remaining -= 0;"),
            ("overlap", sources / "splice.rs",
             b" || span.start < previous_end", b""),
            ("out-of-bounds", sources / "splice.rs",
             b" || span.end > source_len", b""),
            ("reversed-span", sources / "splice.rs",
             b"span.start > span.end || ", b""),
            ("translation-overflow", sources / "splice.rs",
             b"if distance > usize::MAX - destination_start {", b"if false {"),
        ]
        for name, path, before, after in mutations:
            original = originals[path]
            if original.count(before) != 1:
                raise RuntimeError("mutation must locate exactly one executable guard: " + name)
            try:
                path.write_bytes(original.replace(before, after))
                result, text = verify(name)
                if result.returncode == 0 or not re.search(
                    r"verification results::.*\b[1-9][0-9]* errors", text
                ):
                    raise RuntimeError("guard mutation did not fail a proof: " + name + "\n" + text)
            finally:
                path.write_bytes(original)

        result, text = verify("restored-proof")
        results = observations["restored-proof"]["verification_results"]
        if (result.returncode != 0 or not results
                or results[-1]["verified"] < 8 or results[-1]["errors"] != 0):
            raise RuntimeError("restored kernel proof failed:\n" + text)
        observations["passed"] = True
    finally:
        for path, original in originals.items():
            path.write_bytes(original)
        report = {
            "schema": "beans-verus-kernel-verification-v1",
            "pins": pins,
            "source_sha256": {str(path.relative_to(ROOT)): digest(path) for path in originals},
            "lock_sha256": digest(ROOT / "Cargo.lock"),
            "observations": observations,
        }
        (output / "kernel.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
