#!/usr/bin/env python3
"""Read actual Rust-produced raw-frontmatter documents with immutable Go."""
import io
import json
import os
import subprocess
import tarfile
import tempfile
from raw_reader_reference import ROOT, capture


def main():
    work = ROOT / ".compat/raw-reader-cross-read"
    work.mkdir(parents=True, exist_ok=True)
    candidates = work / "rust-documents.json"
    candidates.unlink(missing_ok=True)
    subprocess.run(["cargo", "test", "--locked", "--test", "domain",
                    "raw_reader::raw_yaml_reader_windows_errors_and_ignored_suffix_edits_match_fixed_go", "--", "--exact"],
                   cwd=ROOT, env=dict(os.environ, BN_RUST_RAW_READER_OUTPUT=str(candidates)), check=True)
    corpus = json.loads((ROOT / "tests/contract/raw-reader.json").read_text())
    if capture() != corpus:
        raise SystemExit("Raw reader corpus changed during cross-reading")
    archive = subprocess.check_output(["git", "archive", corpus["source_sha"]], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-raw-reader-reader-") as directory:
        from pathlib import Path
        source = Path(directory)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_raw_reader_test.go").write_bytes((ROOT / "tools/compat/raw_reader_census.go.txt").read_bytes())
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationRawReaderRead$", "-count=1", "-v"], cwd=source,
                       env=dict(os.environ, GOTOOLCHAIN="go1.25.7", BN_RUST_RAW_READER_INPUT=str(candidates)), check=True)
    print(f"fixed Go qualified {len(json.loads(candidates.read_text()))} actual Rust raw-byte documents")


if __name__ == "__main__":
    main()
