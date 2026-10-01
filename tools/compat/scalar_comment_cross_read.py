#!/usr/bin/env python3
"""Read actual Rust-produced scalar comment edits with immutable Go."""
import io
import json
import os
import subprocess
import tarfile
import tempfile
from scalar_comment_reference import ROOT, capture


def main():
    work = ROOT / ".compat/scalar-comment-cross-read"
    work.mkdir(parents=True, exist_ok=True)
    candidates = work / "rust-documents.json"
    candidates.unlink(missing_ok=True)
    subprocess.run(["cargo", "test", "--locked", "--test", "domain",
                    "scalar_comment::scalar_comments_in_utf8_and_utf16_match_fixed_go", "--", "--exact"],
                   cwd=ROOT, env=dict(os.environ, BN_RUST_SCALAR_COMMENT_OUTPUT=str(candidates)), check=True)
    corpus = json.loads((ROOT / "tests/contract/scalar-comment.json").read_text())
    if capture() != corpus:
        raise SystemExit("Raw reader corpus changed during cross-reading")
    archive = subprocess.check_output(["git", "archive", corpus["source_sha"]], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-scalar-comment-reader-") as directory:
        from pathlib import Path
        source = Path(directory)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_scalar_comment_test.go").write_bytes((ROOT / "tools/compat/scalar_comment_census.go.txt").read_bytes())
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationRawReaderRead$", "-count=1", "-v"], cwd=source,
                       env=dict(os.environ, GOTOOLCHAIN="go1.25.7", BN_RUST_RAW_READER_INPUT=str(candidates)), check=True)
    print(f"fixed Go qualified {len(json.loads(candidates.read_text()))} actual Rust raw-byte documents")


if __name__ == "__main__":
    main()
