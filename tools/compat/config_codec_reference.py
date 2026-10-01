#!/usr/bin/env python3
"""Capture configuration TOML writer and loader rules from the fixed Go tree."""
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
    harness = ROOT / "tools/compat/config_codec_census.go.txt"
    with tempfile.TemporaryDirectory(prefix="beans-config-codec-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_config_codec_test.go").write_bytes(harness.read_bytes())
        output = source / "config-codec.json"
        test = "TestMigrationConfigCodecCrossRead" if rust_input else "TestMigrationConfigCodec"
        subprocess.run(["go", "test", "./issue", "-run", "^" + test + "$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_CONFIG_CODEC_OUTPUT=str(output),
                                         BN_CONFIG_CODEC_INPUT=str(Path(rust_input).resolve()) if rust_input else ""),
                       check=True, capture_output=True)
        return {"schema": "beans-config-codec-v1", "source_sha": baseline["source_sha"],
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                **json.loads(output.read_text())}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/config-codec-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Config codec corpus differs from the fixed Go source")
        print("fixed Go config codec corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
