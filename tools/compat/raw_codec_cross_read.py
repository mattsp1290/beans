#!/usr/bin/env python3
"""Read actual Rust-produced raw-body documents with immutable Go."""
import io
import json
import os
import subprocess
import tarfile
import tempfile
from raw_codec_reference import ROOT, capture


def main():
    work = ROOT / ".compat/raw-codec-cross-read"
    work.mkdir(parents=True, exist_ok=True)
    candidates = work / "rust-documents.json"
    candidates.unlink(missing_ok=True)
    subprocess.run(["cargo", "test", "--locked", "--test", "domain",
                    "raw_codec::original_body_bytes_and_mutations_match_fixed_go", "--", "--exact"],
                   cwd=ROOT, env=dict(os.environ, BN_RUST_RAW_CODEC_OUTPUT=str(candidates)), check=True)
    corpus = json.loads((ROOT / "tests/contract/raw-codec.json").read_text())
    if capture() != corpus:
        raise SystemExit("Raw codec corpus changed during cross-reading")
    archive = subprocess.check_output(["git", "archive", corpus["source_sha"]], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-raw-codec-reader-") as directory:
        from pathlib import Path
        source = Path(directory)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_raw_codec_test.go").write_bytes((ROOT / "tools/compat/raw_codec_census.go.txt").read_bytes())
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationRawCodecRead$", "-count=1", "-v"], cwd=source,
                       env=dict(os.environ, GOTOOLCHAIN="go1.25.7", BN_RUST_RAW_CODEC_INPUT=str(candidates)), check=True)
    print(f"fixed Go read and roundtripped {len(json.loads(candidates.read_text()))} actual Rust raw-byte documents")


if __name__ == "__main__":
    main()
