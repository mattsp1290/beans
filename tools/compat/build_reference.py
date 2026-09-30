#!/usr/bin/env python3
"""Build the Go oracle from a fixed revision, never the moving working tree."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BASELINE = ROOT / "tests/contract/baseline.json"


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, capture_output=True, **kwargs).stdout


def digest(data):
    return hashlib.sha256(data).hexdigest()


def build(output, version=None):
    spec = json.loads(BASELINE.read_text())
    if version is not None:
        spec["version"] = version
    output.mkdir(parents=True, exist_ok=True)
    output = output.resolve()
    archive = run(["git", "archive", spec["source_sha"]], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-go-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        binary = output / ("bn-go.exe" if os.name == "nt" else "bn-go")
        run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags",
             f'-X github.com/mattsp1290/beans/version.Version={spec["version"]}',
             "-o", str(binary), "./cmd/bn"], cwd=source)
        expected_version = f'bn version {spec["version"]}\n'.encode()
        if run([str(binary), "--version"]) != expected_version:
            raise RuntimeError("reference VERSION was not linked correctly")
        # This introspection test is never compiled into the reference executable.
        (source / "cmd/bn/migration_census_test.go").write_bytes(
            (ROOT / "tools/compat/census.go.txt").read_bytes())
        env = os.environ.copy()
        env["BN_CENSUS_OUTPUT"] = str(output / "commands.json")
        run(["go", "test", "./cmd/bn", "-run", "^TestMigrationCensus$", "-count=1"],
            cwd=source, env=env)
        metadata = dict(spec, binary_sha256=digest(binary.read_bytes()),
                        os=platform.system(), arch=platform.machine(),
                        go_version=run(["go", "version"]).decode().strip(),
                        git_version=run(["git", "--version"]).decode().strip(),
                        ui_sha256=digest((source / "ui/dist/index.html").read_bytes()))
        (output / "build.json").write_text(json.dumps(metadata, indent=2) + "\n")
    print(binary)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/reference")
    parser.add_argument("--version", help="explicit version override for link verification")
    args = parser.parse_args()
    build(args.output, args.version)
