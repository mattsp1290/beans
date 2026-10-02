#!/usr/bin/env python3
"""Independently recapture and qualify the finite upstream Beans YAML contract."""

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import tarfile

from yaml_upstream_reference import ROOT, ORACLE
from yaml_upstream_report import compare, validate


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def match_reference(candidate, reference):
    validate(candidate)
    if candidate != reference:
        raise ValueError(
            "census identities, bytes, hashes or fixed-Go expectations changed"
        )


def negative_controls(corpus, reference, actual):
    removed = json.loads(json.dumps(corpus))
    removed["rows"].pop()
    altered = json.loads(json.dumps(corpus))
    demo = altered["cases"][0]
    demo["expected"]["model"]["status"] = [0]
    rejected = []
    reasons = {}
    for name, candidate in [
        ("removed upstream row", removed),
        ("altered expectation", altered),
    ]:
        try:
            match_reference(candidate, reference)
        except ValueError as error:
            rejected.append(name)
            reasons[name] = str(error)
        else:
            raise ValueError("negative control accepted: " + name)
    if not any(f["id"] == demo["ID"] for f in compare(altered, actual)):
        raise ValueError("Rust comparison accepted changed expectation")
    return dict(
        rejected=rejected,
        rejection_reasons=reasons,
        mutations=dict(
            removed_row=corpus["rows"][-1]["id"],
            altered_expectation=dict(
                id=demo["ID"],
                field="model.status",
                before=corpus["cases"][0]["expected"]["model"]["status"],
                after=[0],
            ),
        ),
        rust_changed_expectation_rejected=True,
    )


def retain_demo(directory, corpus, actual):
    demo = corpus["cases"][0]
    observed = actual[demo["ID"]]
    before, after = bytes(demo["Input"]), bytes(observed["noop"])
    old, new = b"status: open # retained\n", b"status: in_progress # retained\n"
    start = before.index(old)
    if after != before[:start] + new + before[start + len(old) :]:
        raise ValueError("anchored status edit changed nonowned bytes")
    if observed["model"] != observed["reread"]["model"]:
        raise ValueError("anchored full model changed on production reread")
    if observed["noop"] != observed["reread"]["noop"]:
        raise ValueError("anchored reread rewrites edited bytes")
    for phase in [observed, observed["initial_read"], observed["reread"]]:
        if phase["parse_error"] or phase["encode_error"]:
            raise ValueError(
                "anchored demonstration failed a production read or encode"
            )
    note = directory / demo["Path"]
    if note.read_bytes() != before:
        raise ValueError("persisted hub-note bytes differ from observed input")
    (directory / "after.md").write_bytes(after)
    write_json(directory / "go-observations.json", demo["expected"])
    write_json(directory / "rust-observations.json", observed)
    proof = dict(
        id=demo["ID"],
        original_path=demo["Path"],
        source_status_span=[start, start + len(old)],
        destination_status_span=[start, start + len(new)],
        unchanged_ranges=[
            [0, start, 0, start],
            [start + len(old), len(before), start + len(new), len(after)],
        ],
        before_sha256=hashlib.sha256(before).hexdigest(),
        after_sha256=hashlib.sha256(after).hexdigest(),
        full_go_rust_equal=observed == demo["expected"],
        full_reread_model_equal=True,
        outside_splice_bytes_equal=True,
    )
    for a, b, c, d in proof["unchanged_ranges"]:
        if before[a:b] != after[c:d]:
            raise ValueError("outside-splice proof failed")
    write_json(directory / "splice-proof.json", proof)
    return proof


def command(argv, log, env):
    result = subprocess.run(
        argv,
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    log.write_text(result.stdout)
    print(result.stdout, end="", flush=True)
    if result.returncode:
        raise RuntimeError(f"command exited {result.returncode}: {' '.join(argv)}")


def replay_note(directory, corpus, env):
    """Read a persisted hub fixture and feed its exact bytes to both real readers."""
    directory.mkdir()
    demo = dict(corpus["cases"][0])
    note = directory / demo["Path"]
    note.parent.mkdir(parents=True)
    note.write_bytes(bytes(demo["Input"]))
    demo["Input"] = list(note.read_bytes())
    go_input = directory / "go-input.json"
    rust_input = directory / "rust-input.json"
    write_json(go_input, [demo])
    write_json(rust_input, dict(cases=[demo]))
    go_output = directory / "go-journey-observations.json"
    rust_output = directory / "rust-journey-observations.json"
    archive = subprocess.check_output(["git", "archive", ORACLE], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-yaml-note-oracle-") as temporary:
        source = Path(temporary)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        harness = (ROOT / "tools/compat/yaml-upstream/issue.go.txt").read_bytes()
        harness += (ROOT / "tools/compat/yaml-upstream/common.go.txt").read_bytes()
        (source / "issue/migration_yaml_test.go").write_bytes(harness)
        result = subprocess.run(
            ["go", "test", "./issue", "-run", "^TestMigrationYAMLCensus$", "-count=1"],
            cwd=source,
            env=dict(env, BN_YAML_INPUT=str(go_input), BN_YAML_OUTPUT=str(go_output)),
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        (directory / "go-journey.log").write_text(result.stdout)
        if result.returncode:
            raise RuntimeError(
                "immutable Go persisted-note journey failed: " + result.stdout
            )
    command(
        [
            "cargo",
            "test",
            "--locked",
            "--test",
            "domain",
            "yaml_upstream_observations",
            "--",
            "--nocapture",
        ],
        directory / "rust-journey.log",
        dict(env, BN_YAML_CORPUS=str(rust_input), BN_YAML_RUST_OUTPUT=str(rust_output)),
    )
    go, rust = json.loads(go_output.read_text()), json.loads(rust_output.read_text())
    if set(go) != {demo["ID"]} or set(rust) != {demo["ID"]}:
        raise ValueError("persisted-note journey omitted or added observations")
    if go != rust or go[demo["ID"]] != demo["expected"]:
        raise ValueError("persisted-note full initial/edit/reread results differ")
    return rust


def qualify(corpus_path):
    evidence_root = ROOT / ".compat/yaml-upstream"
    evidence_root.mkdir(parents=True, exist_ok=True)
    evidence = Path(tempfile.mkdtemp(prefix="run-", dir=evidence_root))
    env = dict(os.environ, GOTOOLCHAIN="go1.25.7")
    identity = dict(
        schema="beans-yaml-upstream-gate-v1",
        status="running",
        head=subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
        ).strip(),
        working_tree_clean=not subprocess.check_output(
            ["git", "status", "--porcelain"], cwd=ROOT, text=True
        ).strip(),
        gate_sha256=digest(Path(__file__)),
        reference_driver_sha256=digest(
            ROOT / "tools/compat/yaml_upstream_reference.py"
        ),
        report_driver_sha256=digest(ROOT / "tools/compat/yaml_upstream_report.py"),
        corpus_file=str(corpus_path),
        platform=platform.platform(),
        oracle_sha=ORACLE,
        go=subprocess.check_output(["go", "version"], env=env, text=True).strip(),
        rust=subprocess.check_output(["rustc", "--version"], text=True).strip(),
        cargo_target_dir=os.environ.get("CARGO_TARGET_DIR", "workspace target"),
    )
    write_json(evidence / "identity.json", identity)
    write_json(
        evidence_root / "latest.json",
        dict(directory=str(evidence.relative_to(ROOT)), **identity),
    )
    try:
        if platform.system() != "Linux":
            raise ValueError("this qualification gate requires Linux")
        # Populate the immutable dependency even on a fresh CI Go module cache.
        command(
            ["go", "mod", "download", "gopkg.in/yaml.v3@v3.0.1"],
            evidence / "dependency.log",
            env,
        )
        recapture = evidence / "go-recapture.json"
        command(
            [
                sys.executable,
                "-S",
                "tools/compat/yaml_upstream_reference.py",
                "--output",
                str(recapture),
            ],
            evidence / "recapture.log",
            env,
        )
        corpus, reference = json.loads(corpus_path.read_text()), json.loads(
            recapture.read_text()
        )
        match_reference(corpus, reference)
        report, rust_output = (
            evidence / "report.json",
            evidence / "rust-observations.json",
        )
        command(
            [
                sys.executable,
                "-S",
                "tools/compat/yaml_upstream_report.py",
                "--corpus",
                str(corpus_path),
                "--report",
                str(report),
                "--rust-output",
                str(rust_output),
            ],
            evidence / "comparison.log",
            env,
        )
        actual = json.loads(rust_output.read_text())
        result = json.loads(report.read_text())
        if result["observations"] != 12546 or result["mismatches"] != 0:
            raise ValueError("incomplete or mismatching production observations")
        controls = negative_controls(corpus, reference, actual)
        write_json(evidence / "negative-controls.json", controls)
        journey = replay_note(evidence / "demo", corpus, env)
        if journey[corpus["cases"][0]["ID"]] != actual[corpus["cases"][0]["ID"]]:
            raise ValueError("standalone note journey differs from full qualification")
        demonstration = retain_demo(evidence / "demo", corpus, journey)
        identity.update(
            status="passed",
            observations=result["observations"],
            mismatches=result["mismatches"],
            counts=result["counts"],
            source_sha256=corpus["source_sha256"],
            dependency=corpus["dependency"],
            dependency_go_mod_sha256=corpus["dependency_go_mod_sha256"],
            license_sha256=corpus["license_sha256"],
            harness_sha256=corpus["harness_sha256"],
            corpus_sha256=digest(corpus_path),
            recapture_sha256=digest(recapture),
            report_sha256=digest(report),
            negative_controls=controls,
            demo=demonstration,
        )
    except Exception as error:
        identity.update(status="failed", error=str(error))
        raise
    finally:
        identity["artifact_sha256"] = {
            str(p.relative_to(evidence)): digest(p)
            for p in sorted(evidence.rglob("*"))
            if p.is_file() and p.name != "identity.json"
        }
        write_json(evidence / "identity.json", identity)
        write_json(
            evidence_root / "latest.json",
            dict(directory=str(evidence.relative_to(ROOT)), **identity),
        )
        print("Qualification evidence:", evidence)
    print(
        f"Qualified {identity['observations']} production observations; zero mismatches; both negative controls rejected"
    )
    print("Exact head:", identity["head"], "report SHA256:", identity["report_sha256"])
    return identity


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--corpus", type=Path, default=ROOT / "tests/contract/yaml-upstream.json"
    )
    args = parser.parse_args()
    qualify(args.corpus.resolve())
