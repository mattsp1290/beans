#!/usr/bin/env python3
"""Qualify ordinary Cargo and Verus on the same development-only source artifact."""
import json
import os
from pathlib import Path
import re
import subprocess

from install import PINS, ROOT, digest, install


def main():
    pins = json.loads(PINS.read_text())
    output = ROOT / ".verification"
    output.mkdir(parents=True, exist_ok=True)
    tools = install(ROOT / ".verification/verus")
    env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
               RUSTUP_TOOLCHAIN=pins["rust"], VERUS_Z3_PATH=str(tools / "z3"),
               CARGO_TARGET_DIR=str(output / "target"))
    probe = ROOT / "tools/verification/probe"
    observations = {}
    def run(name, argv, timeout=360):
        result = subprocess.run(argv, cwd=probe, env=env, capture_output=True, timeout=timeout)
        text = (result.stdout + result.stderr).decode(errors="replace")
        (output / (name + ".log")).write_text(text)
        observations[name] = {"argv": argv, "exit": result.returncode, "log": name + ".log"}
        print(name + ": exit " + str(result.returncode), flush=True)
        return result, text
    try:
        for name, argv, expected in (
            ("rust", ["rustc", "--version"], "rustc " + pins["rust"] + " "),
            ("z3", [str(tools / "z3"), "--version"], "Z3 version " + pins["z3_version"] + " "),
            ("verus", [str(tools / "verus"), "--version"], pins["version"]),
        ):
            result, text = run(name, argv)
            if result.returncode != 0 or expected not in text:
                raise RuntimeError("unexpected pinned tool version: " + name + "\n" + text)
        for name, argv in (
            ("cargo-build", ["cargo", "build", "--locked"]),
            ("cargo-test", ["cargo", "test", "--locked"]),
            ("cargo-verus", ["cargo", "verus", "verify", "--locked"]),
        ):
            result, text = run(name, argv)
            if result.returncode != 0:
                raise RuntimeError(name + " failed:\n" + text)
        source = probe / "src/lib.rs"
        original = source.read_bytes()
        broken = original.replace(b"else { value + 1 }", b"else { value }")
        if broken == original:
            raise RuntimeError("probe mutation no longer locates executable body")
        try:
            source.write_bytes(broken)
            result, text = run("mutated-proof", ["cargo", "verus", "verify", "--locked"])
            if result.returncode == 0 or not re.search(r"verification results::.*\b[1-9][0-9]* errors", text):
                raise RuntimeError("body mutation was not rejected as a proof failure:\n" + text)
        finally:
            source.write_bytes(original)
        result, text = run("restored-proof", ["cargo", "verus", "verify", "--locked"])
        if result.returncode != 0:
            raise RuntimeError("restored proof failed:\n" + text)
        observations["passed"] = True
    finally:
        report = {"schema": "beans-verus-toolchain-qualification-v1", "pins": pins,
                  "probe_sha256": digest(probe / "src/lib.rs"),
                  "lock_sha256": digest(probe / "Cargo.lock"), "observations": observations}
        (output / "qualification.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
