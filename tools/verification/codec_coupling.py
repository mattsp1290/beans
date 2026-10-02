#!/usr/bin/env python3
"""Observe actual codec calls to verified splice bodies using pinned LLVM coverage."""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
HELPERS = {"valid_splices": 4, "preserved_interval": 3, "translate_offset": 3}
CASES = [f"{kind}_{encoding}" for kind in ("issue", "request", "memory", "handoff")
         for encoding in ("utf8", "utf16le", "utf16be")] + ["plan_graph_utf8"]
SOURCE = ROOT / "crates/beans-kernel/src/splice.rs"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def toolchain():
    version = subprocess.check_output(["rustc", "-vV"], cwd=ROOT, text=True)
    info = dict(line.split(": ", 1) for line in version.splitlines() if ": " in line)
    if info["release"] != "1.98.1":
        raise SystemExit("codec coupling requires pinned Rust1.98.1")
    sysroot = Path(subprocess.check_output(["rustc", "--print", "sysroot"], cwd=ROOT, text=True).strip())
    llvm = sysroot / "lib/rustlib" / info["host"] / "bin"
    for name in ("llvm-profdata", "llvm-cov"):
        if not (llvm / name).is_file():
            raise SystemExit("missing pinned LLVM tools; run rustup component add llvm-tools --toolchain 1.98.1")
    return info, llvm


def build(work):
    profiles = work / "build-profiles"
    profiles.mkdir()
    env = dict(os.environ, CARGO_TARGET_DIR=str(ROOT / ".verification/codec-coupling-build"),
               RUSTFLAGS="-Cinstrument-coverage", CARGO_INCREMENTAL="0",
               LLVM_PROFILE_FILE=str(profiles / "%p-%m.profraw"))
    with (work / "build.log").open("w") as log:
        result = subprocess.run(["cargo", "test", "--locked", "--test", "domain", "--no-run", "--message-format=json"],
                                cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=log, text=True, check=True)
    artifacts = [json.loads(line) for line in result.stdout.splitlines() if line.startswith("{")]
    executables = [Path(a["executable"]) for a in artifacts if a.get("reason") == "compiler-artifact"
                   and a.get("executable") and a["target"]["name"] == "domain"]
    if len(executables) != 1:
        raise SystemExit("expected exactly one instrumented domain integration binary")
    return executables[0]


def observe(binary, llvm, work, case):
    folder = work / case
    folder.mkdir()
    env = dict(os.environ, LLVM_PROFILE_FILE=str(folder / "%p-%m.profraw"))
    selector = f"splice_coupling::{case}"
    with (folder / "test.log").open("w") as log:
        subprocess.run([str(binary), selector, "--exact", "--nocapture"], cwd=ROOT,
                       env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    if "1 passed; 0 failed" not in (folder / "test.log").read_text():
        raise SystemExit(f"coupling case did not execute exactly one passing test: {case}")
    raw = sorted(folder.glob("*.profraw"))
    if not raw:
        raise SystemExit(f"no compiler coverage produced for {case}")
    profile = folder / "coverage.profdata"
    subprocess.run([str(llvm / "llvm-profdata"), "merge", "--sparse", *map(str, raw), "-o", str(profile)], check=True)
    output = subprocess.check_output([str(llvm / "llvm-cov"), "export", str(binary),
                                      f"--instr-profile={profile}", "--name-regex=" + "|".join(HELPERS),
                                      str(SOURCE)], text=True)
    (folder / "coverage.json").write_text(output)
    functions = [f for data in json.loads(output)["data"] for f in data["functions"]
                 if any(Path(name).resolve() == SOURCE for name in f["filenames"])]
    observed = {}
    for helper in HELPERS:
        matches = [f for f in functions if helper in f["name"]]
        if len(matches) != 1:
            raise SystemExit(f"expected one actual kernel body for {helper}, got {len(matches)}")
        observed[helper] = {"count": matches[0]["count"], "symbol": matches[0]["name"]}
    return {"test": selector, "helpers": observed}


def qualifies(record):
    counts = {"valid_splices": 3, "preserved_interval": 2, "translate_offset": 2} \
        if record["test"].endswith("::plan_graph_utf8") else HELPERS
    return all(record["helpers"][name]["count"] == count for name, count in counts.items())


def callers():
    found = []
    for path in sorted((ROOT / "src").rglob("*.rs")):
        for match in re.finditer(r"\bbyte_edit::apply\s*\(", path.read_text()):
            found.append({"file": str(path.relative_to(ROOT)),
                          "line": path.read_text()[:match.start()].count("\n") + 1,
                          "sha256": sha(path)})
    expected = ["src/domain/frontmatter.rs", "src/domain/plan/reference.rs"]
    if [entry["file"] for entry in found] != expected:
        raise SystemExit(f"splice caller inventory changed; extend coupling cases: {found}")
    return found


def main():
    base = ROOT / ".verification/codec-coupling"
    base.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="run-", dir=base))
    info, llvm = toolchain()
    binary = build(work)
    records = []
    for case in CASES:
        record = observe(binary, llvm, work, case)
        if not qualifies(record):
            raise SystemExit(f"production coupling counts differ for {case}: {record}")
        records.append(record)
    negative = observe(binary, llvm, work, "parse_only_negative_control")
    if qualifies(negative) or any(v["count"] for v in negative["helpers"].values()):
        raise SystemExit("parse-only negative control unexpectedly reaches splice bodies")
    report = {"schema": "beans-codec-kernel-coupling-v1", "os": platform.system(), "arch": platform.machine(),
              "rust": info, "compiler_flags": ["-Cinstrument-coverage"],
              "git_sha": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
              "splice_adapter_source_sha256": sha(ROOT / "src/domain/byte_edit.rs"),
              "span_construction_source_sha256": sha(ROOT / "src/domain/splicing.rs"),
              "kernel_source": str(SOURCE.relative_to(ROOT)), "kernel_source_sha256": sha(SOURCE),
              "cargo_lock_sha256": sha(ROOT / "Cargo.lock"), "binary_sha256": sha(binary),
              "test_source_sha256": sha(ROOT / "tests/domain/splice_coupling.rs"),
              "application_callers": callers(),
              "cases": records, "negative_control": dict(negative, rejected=True),
              "scope": "actual compiled helper entry counts plus integration assertions of parsed raw ranges and copied bytes; not a parser or whole-codec proof"}
    (work / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    (base / "latest.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"qualified {len(records)} production codec cases; parse-only control rejected; evidence {work / 'report.json'}")


if __name__ == "__main__":
    main()
