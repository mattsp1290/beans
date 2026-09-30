#!/usr/bin/env python3
"""Build the Go oracle from a fixed revision, never the moving working tree."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BASELINE = ROOT / "tests/contract/baseline.json"


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, capture_output=True, **kwargs).stdout


def digest(data):
    return hashlib.sha256(data).hexdigest()


def build(output, version=None, ui_build=False):
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
        ui_metadata = {}
        if ui_build:
            node = run(["node", "--version"]).decode().strip()
            npm = run(["npm", "--version"]).decode().strip()
            if (node, npm) != ("v24.12.0", "11.6.2"):
                raise RuntimeError("UI oracle requires Node v24.12.0 and npm 11.6.2")
            env = {k: v for k, v in os.environ.items() if not k.startswith("VITE_")}
            env.update(LANG="C", LC_ALL="C", TZ="UTC")
            run(["npm", "ci", "--no-audit", "--no-fund"], cwd=source / "ui", env=env, timeout=180)
            run(["npm", "run", "build"], cwd=source / "ui", env=env, timeout=120)
            assets = {p.relative_to(source / "ui/dist").as_posix(): digest(p.read_bytes())
                      for p in sorted((source / "ui/dist").rglob("*")) if p.is_file()}
            shutil.copytree(source / "ui/dist", output / "ui", dirs_exist_ok=True)
            spec["ui"] = "built"
            ui_metadata = {"node_version": node, "npm_version": npm, "ui_assets": assets,
                           "ui_lock_sha256": digest((source / "ui/package-lock.json").read_bytes())}
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
        metadata.update(ui_metadata)
        (output / "build.json").write_text(json.dumps(metadata, indent=2) + "\n")
    print(binary)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/reference")
    parser.add_argument("--version", help="explicit version override for link verification")
    parser.add_argument("--ui-build", action="store_true", help="embed the pinned full UI build")
    args = parser.parse_args()
    build(args.output, args.version, args.ui_build)
