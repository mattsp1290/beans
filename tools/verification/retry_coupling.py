#!/usr/bin/env python3
"""Observe production Git transactions entering the verified retry kernel.

The real-Git scenarios assert history, nonce ownership, retained authored bytes
and the push bound; LLVM evidence closes the production-call coupling gap.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import tempfile

from codec_coupling import ROOT, toolchain

SOURCE = ROOT / "crates/beans-kernel/src/retry.rs"
HELPERS = ["decide_retry", "take_push_attempt"]
CASES = ["push_conflict_replays_owned_head_without_duplicating_notes",
         "conflict_preserves_unowned_history_even_with_matching_subject",
         "repeated_races_exhaust_three_pushes_and_preserve_last_commit",
         "stranded_previous_nonce_never_authorizes_discard_in_new_invocation"]
NEGATIVE = "git_resolver::resolver_fake_records_exact_root_and_preserves_configured_capture"


def observe(binary, llvm, work, selector):
    folder = work / selector.replace("::", "-")
    folder.mkdir()
    env = dict(os.environ, LLVM_PROFILE_FILE=str(folder / "%p-%m.profraw"))
    with (folder / "test.log").open("w") as log:
        subprocess.run([str(binary), selector, "--exact", "--nocapture"], cwd=ROOT,
                       env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    if "1 passed; 0 failed" not in (folder / "test.log").read_text():
        raise RuntimeError(f"missing exact executed coupling test: {selector}")
    profile = folder / "coverage.profdata"
    subprocess.run([str(llvm / "llvm-profdata"), "merge", "--sparse",
                    *map(str, sorted(folder.glob("*.profraw"))), "-o", str(profile)], check=True)
    result = subprocess.check_output([str(llvm / "llvm-cov"), "export", str(binary),
                                      f"--instr-profile={profile}", "--name-regex=" + "|".join(HELPERS),
                                      str(SOURCE)], text=True)
    (folder / "coverage.json").write_text(result)
    functions = [f for d in json.loads(result)["data"] for f in d["functions"]
                 if any(Path(name).resolve() == SOURCE for name in f["filenames"])]
    counts = {}
    for helper in HELPERS:
        found = [f for f in functions if helper in f["name"]]
        if len(found) != 1:
            raise RuntimeError(f"expected exactly one compiled retry helper {helper}")
        counts[helper] = found[0]["count"]
    return {"test": selector, "helpers": counts}


def main():
    info, llvm = toolchain()
    base = ROOT / ".verification/retry-coupling"
    base.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="run-", dir=base))
    profiles = work / "build-profiles"
    profiles.mkdir()
    env = dict(os.environ, CARGO_TARGET_DIR=str(ROOT / ".verification/codec-coupling-build"),
               RUSTFLAGS="-Cinstrument-coverage", CARGO_INCREMENTAL="0",
               LLVM_PROFILE_FILE=str(profiles / "%p-%m.profraw"))
    with (work / "build.log").open("w") as log:
        build = subprocess.run(["cargo", "test", "--locked", "--test", "native_cli", "--no-run", "--message-format=json"],
                               cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=log, text=True, check=True)
    records = [json.loads(line) for line in build.stdout.splitlines() if line.startswith("{")]
    binaries = [r["executable"] for r in records if r.get("reason") == "compiler-artifact"
                and r.get("executable") and r["target"]["name"] == "native_cli"]
    if len(binaries) != 1:
        raise RuntimeError("expected one native CLI test executable")
    binary = Path(binaries[0])
    cases = [observe(binary, llvm, work, selector) for selector in CASES]
    for record in cases:
        if any(record["helpers"][helper] <= 0 for helper in HELPERS):
            raise RuntimeError(f"production transaction failed to call retry kernel: {record}")
    negative = observe(binary, llvm, work, NEGATIVE)
    if any(negative["helpers"].values()):
        raise RuntimeError("read-only resolver control unexpectedly invoked retry effects")
    caller = ROOT / "src/gitops/hub.rs"
    for helper in HELPERS:
        if helper + "(" not in caller.read_text():
            raise RuntimeError(f"production caller no longer invokes {helper}; review coupling")
    sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
    report = {"schema": "beans-retry-kernel-coupling-v1", "os": platform.system(), "arch": platform.machine(),
              "rust": info, "git_sha": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
              "kernel_source_sha256": sha(SOURCE), "production_source_sha256": sha(caller),
              "binary_sha256": sha(binary), "cargo_lock_sha256": sha(ROOT / "Cargo.lock"),
              "cases": cases, "negative_control": dict(negative, rejected=True),
              "scope": "Actual compiled retry helper entry counts from production real-Git journeys; does not prove Git, filesystem, parser, or entire transaction correctness."}
    (work / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    (base / "latest.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"qualified {len(cases)} production retry journeys; read-only control rejected; evidence {work / 'report.json'}")


if __name__ == "__main__":
    main()
