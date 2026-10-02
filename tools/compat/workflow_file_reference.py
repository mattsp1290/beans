#!/usr/bin/env python3
"""Capture workflow file decoding and source precedence from the fixed Go tree."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def capture(rust_input=None):
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    harness = ROOT / "tools/compat/workflow_file_census.go.txt"
    writer_corpus = ROOT / "tests/contract/config-codec.json"
    if json.loads(writer_corpus.read_text())["source_sha"] != baseline["source_sha"]:
        raise SystemExit("Project writer corpus does not use the immutable baseline")
    with tempfile.TemporaryDirectory(prefix="beans-workflow-file-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_workflow_file_test.go").write_bytes(harness.read_bytes())
        output = source / "workflow-file.json"
        test = "TestMigrationWorkflowFileCrossRead" if rust_input else "TestMigrationWorkflowFile"
        subprocess.run(["go", "test", "./issue", "-run", "^" + test + "$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_WORKFLOW_FILE_OUTPUT=str(output),
                                         BN_WORKFLOW_PROJECT_INPUT=str(writer_corpus),
                                         BN_WORKFLOW_RUST_INPUT=str(Path(rust_input).resolve()) if rust_input else ""),
                       check=True, capture_output=True)
        return {"schema": "beans-workflow-file-v1", "source_sha": baseline["source_sha"],
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                "project_writer_corpus_sha256": hashlib.sha256(writer_corpus.read_bytes()).hexdigest(),
                **json.loads(output.read_text())}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/workflow-file-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Workflow file corpus differs from the fixed Go source")
        print("fixed Go workflow file corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
