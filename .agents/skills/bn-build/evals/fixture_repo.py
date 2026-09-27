#!/usr/bin/env python3
"""Real, offline Git/Beans fixture. Every mutable evaluation resource is disposable."""
from __future__ import annotations

import json
import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import tempfile

REPOSITORY = Path(__file__).resolve().parents[4]
HELPER = Path(__file__).resolve().parents[1] / "scripts/mvp_state.py"


def isolated_env(root: Path, actor: str = "builder") -> dict[str, str]:
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(("BEANS_", "BN_", "GIT_CONFIG_")) or key in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"):
            env.pop(key)
    env.update({"BEANS_HOME": str(root / actor / "beans-home"),
                "BEANS_HUB": str(root / actor / "hub"), "BEANS_PROJECT": "fixture",
                "BN_ACTOR": actor, "GIT_CONFIG_GLOBAL": str(root / "gitconfig"),
                "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0",
                "GIT_ALLOW_PROTOCOL": "file", "PYTHONDONTWRITEBYTECODE": "1"})
    return env


def build_bn(root: Path) -> Path:
    """Build evaluated source; offline dependency resolution, isolated build cache."""
    binary = root / "bin/bn"
    binary.parent.mkdir(parents=True, exist_ok=True)
    env = isolated_env(root)
    env.update({"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOCACHE": str(root / "go-build-cache")})
    # Invoke an already installed module toolchain directly: auto-toolchain mode
    # otherwise insists on checksum-service verification even when cached.
    modcache = subprocess.run(["go", "env", "GOMODCACHE"], env=env, text=True,
                              capture_output=True, check=True).stdout.strip()
    version = re.search(r"^go (\S+)$", (REPOSITORY / "go.mod").read_text(), re.M).group(1)
    candidates = sorted(Path(modcache).glob(f"golang.org/toolchain@v*-go{version}.*/bin/go"))
    compiler = str(candidates[0]) if candidates else "go"
    result = subprocess.run([compiler, "build", "-o", str(binary), "./cmd/bn"], cwd=REPOSITORY,
                            env=env, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError("offline source build failed: " + result.stderr)
    return binary


class Fixture:
    def __init__(self, root: Path, binary: Path, default: str = "main"):
        self.root = root.resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.binary = binary.resolve()
        self.trace = self.root / "trace.jsonl"
        self.envs = {name: isolated_env(self.root, name) for name in ("builder", "auditor")}
        self.sources = {name: self.root / name / "source" for name in self.envs}
        self.code_remote = self.root / "code.git"
        self.hub_remote = self.root / "hub.git"
        self.default = default
        (self.root / "gitconfig").write_text("[user]\n name = Fixture\n email = fixture@example.invalid\n[init]\n defaultBranch = main\n")
        # A simulated external default is deliberately invalid if ever loaded.
        self.sentinel = self.root / "external-defaults"
        self.sentinel.mkdir()
        (self.sentinel / "config.toml").write_text("DO NOT READ OR CHANGE\n")
        (self.sentinel / "cache").write_text("DO NOT CHANGE\n")
        self.sentinel_before = self.sentinel_bytes()
        for name, env in self.envs.items():
            for key in ("BEANS_HOME", "BEANS_HUB", "GIT_CONFIG_GLOBAL"):
                self.assert_inside(Path(env[key]))
            self.assert_inside(Path(env["BEANS_HOME"]) / "config.toml")
            self.assert_inside(Path(env["BEANS_HOME"]) / "cache")
            if "BN_CONFIG" in env:
                raise AssertionError("inherited BN_CONFIG leaked")
            self.sources[name].parent.mkdir()
        self.git("init", "--bare", str(self.code_remote), cwd=self.root)
        self.git("init", "--bare", str(self.hub_remote), cwd=self.root)
        source = self.sources["builder"]
        source.mkdir()
        self.git("init", "-b", default)
        (source / "README.md").write_text("Disposable MVP evaluation\n")
        self.git("add", "README.md")
        self.git("commit", "-m", "fixture: initial source")
        self.git("remote", "add", "origin", str(self.code_remote))
        self.git("push", "-u", "origin", default)
        self.git("symbolic-ref", "HEAD", "refs/heads/" + default, cwd=self.code_remote)
        self.git("clone", str(self.code_remote), str(self.sources["auditor"]), cwd=self.root)
        self.base_sha = self.git("rev-parse", "HEAD").stdout.strip()
        self.default_before = self.remote_sha("refs/heads/" + default)
        for actor in self.envs:
            init = self.bn("init", str(self.hub_remote), actor=actor)
            self.assert_inside(Path(init["config"]))
            self.assert_inside(Path(init["hub"]))
            config = Path(init["config"])
            # Long throttle deterministically demonstrates stale read behavior.
            config.write_text(re.sub(r'throttle = "[^"]*"', 'throttle = "1h"', config.read_text()))
            if actor == "builder":
                self.bn("project", "create", "fixture")
        self.root_id = self.bn("create", "Fixture milestone", "--type", "epic")["id"]
        self.first = self.bn("create", "First slice", "--parent", self.root_id,
                             "--label", "mvp:" + self.root_id, "--label", "mvp:must")["id"]
        self.second = self.bn("create", "Second slice", "--parent", self.root_id,
                              "--label", "mvp:" + self.root_id, "--label", "mvp:must")["id"]
        self.bn("dep", "add", self.second, self.first)
        self.contract = self.make_contract()
        self.contract_path = self.write_json("contract.json", self.contract)
        description = "```bn-mvp-contract\n" + json.dumps(self.contract) + "\n```"
        self.bn("update", self.root_id, "--description", description)
        self.bn("sync", actor="auditor")

    def assert_inside(self, path: Path) -> None:
        if not path.resolve().is_relative_to(self.root):
            raise AssertionError(f"fixture path escaped: {path}")

    def sentinel_bytes(self) -> dict[str, bytes]:
        return {p.name: p.read_bytes() for p in self.sentinel.iterdir()}

    def run(self, argv: list[str], actor: str = "builder", cwd: Path | None = None,
            check: bool = True, timeout: int = 30) -> subprocess.CompletedProcess:
        started = time.monotonic()
        result = subprocess.run(argv, cwd=cwd or self.sources[actor], env=self.envs[actor],
                                text=True, capture_output=True, timeout=timeout)
        with self.trace.open("a") as stream:
            stream.write(json.dumps({"argv": argv, "actor": actor, "cwd": str(cwd or self.sources[actor]),
                                    "returncode": result.returncode, "stdout": result.stdout,
                                    "stderr": result.stderr, "elapsed": time.monotonic() - started}) + "\n")
        if check and result.returncode:
            raise AssertionError(f"{argv!r} failed ({result.returncode}): {result.stdout}\n{result.stderr}")
        return result

    def git(self, *args: str, actor: str = "builder", cwd: Path | None = None,
            check: bool = True) -> subprocess.CompletedProcess:
        return self.run(["git", *args], actor=actor, cwd=cwd, check=check)

    def bn(self, *args: str, actor: str = "builder"):
        result = self.run([str(self.binary), *args, "--json"], actor=actor)
        return json.loads(result.stdout)

    def state(self, op: str, *args: str, actor: str = "builder", check: bool = True):
        result = self.run([sys.executable, str(HELPER), op, "--repo", str(self.sources[actor]),
                           "--contract", str(self.contract_path), *args], actor=actor, check=check)
        return result, json.loads(result.stdout)

    def write_json(self, name: str, value) -> Path:
        path = self.root / name
        path.write_text(json.dumps(value))
        return path

    def remote_sha(self, ref: str) -> str:
        return self.git("rev-parse", ref, cwd=self.code_remote).stdout.strip()

    def make_contract(self) -> dict:
        return {"version": 1, "milestone_id": self.root_id, "key": "fixture-mvp", "project": "fixture",
                "repository": str(self.code_remote), "remote": "origin", "target_ref": "refs/heads/mvp/fixture",
                "default_ref": "refs/heads/" + self.default, "base_sha": self.base_sha, "scope_revision": 1,
                "outcome": "Read two integrated fixture files", "non_goals": ["Deployment"],
                "context": {"active_users": False, "preserve_compatibility": True, "source": "Disposable brief"},
                "authority": {"integrate_target": True, "main_merge": False, "deploy": False, "source": "Fixture authority"},
                "acceptance": ["Both fixture files can be read"],
                "checks": [{"argv": [sys.executable, "-c", "print('fixture check passed')"], "cwd": "."}],
                "prerequisites": [], "runtime_identity": "Git HEAD", "max_in_flight": 2,
                "slices": [{"id": self.first, "outcome": "First file", "depends_on": [], "areas": ["."],
                            "acceptance": ["First file exists"], "risk": "routine"},
                           {"id": self.second, "outcome": "Second file", "depends_on": [self.first], "areas": ["."],
                            "acceptance": ["Second file exists"], "risk": "routine"}]}

    def verify_isolation(self) -> None:
        if self.sentinel_bytes() != self.sentinel_before:
            raise AssertionError("external defaults changed")
        if self.remote_sha("refs/heads/" + self.default) != self.default_before:
            raise AssertionError("default branch changed")
        for actor in self.envs:
            for repository in (self.sources[actor], Path(self.envs[actor]["BEANS_HUB"])):
                for remote in self.git("remote", actor=actor, cwd=repository).stdout.splitlines():
                    url = self.git("remote", "get-url", remote, actor=actor, cwd=repository).stdout.strip()
                    self.assert_inside(Path(url))

    def export(self) -> dict:
        """Enough explicit context for a separately recorded forward agent run."""
        keys = ("BEANS_HOME", "BEANS_HUB", "BEANS_PROJECT", "BN_ACTOR", "GIT_CONFIG_GLOBAL",
                "GIT_CONFIG_NOSYSTEM", "GIT_TERMINAL_PROMPT", "GIT_ALLOW_PROTOCOL", "PYTHONDONTWRITEBYTECODE")
        return {"root": str(self.root), "isolation_root": str(self.binary.parent.parent),
                "bn": str(self.binary), "contract": str(self.contract_path),
                "milestone_id": self.root_id, "slice_ids": [self.first, self.second],
                "code_remote": str(self.code_remote), "hub_remote": str(self.hub_remote),
                "default_sha": self.default_before,
                "actors": {actor: {"source": str(self.sources[actor]),
                                   "env": {key: env[key] for key in keys},
                                   "unset": ["BN_CONFIG", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"]}
                           for actor, env in self.envs.items()}}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, help="fresh parent for disposable binary and fixture")
    arguments = parser.parse_args()
    root = arguments.output or Path(tempfile.mkdtemp(prefix="bn-mvp-forward-"))
    root.mkdir(parents=True, exist_ok=True)
    if any(root.iterdir()):
        raise SystemExit("--output must be empty")
    fixture = Fixture(root / "fixture", build_bn(root))
    fixture.verify_isolation()
    exported = fixture.export()
    (root / "context.json").write_text(json.dumps(exported, indent=2) + "\n")
    print(json.dumps(exported, indent=2))
