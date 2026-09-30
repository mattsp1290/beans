#!/usr/bin/env python3
"""Capture or check byte-exact CLI contracts using isolated, offline fixtures."""
import argparse
import base64
from contextlib import ExitStack
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

from terminal import capture
from reference import authenticate_capture

ROOT = Path(__file__).resolve().parents[2]
CONTRACT = ROOT / "tests/contract"


def encode(data):
    return base64.b64encode(data).decode("ascii")


def environment(root):
    # Deliberately do not inherit user Beans/Git/color/config environment.
    return {"PATH": os.environ["PATH"], "HOME": str(root / "user"),
            "USER": "contract", "LANG": "C", "LC_ALL": "C", "TZ": "UTC",
            "TERM": "dumb", "NO_COLOR": "1", "COLUMNS": "80", "LINES": "24",
            "BEANS_HOME": str(root / "beans"), "BEANS_HUB": str(root / "hub"),
            "BN_ACTOR": "contract", "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": str(root / "user/gitconfig"),
            # Newer Git fetch defaults create origin/HEAD; pin fixture behavior
            # explicitly instead of dropping that ref from state comparisons.
            "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "remote.origin.followRemoteHEAD",
            "GIT_CONFIG_VALUE_0": "never",
            "GIT_TERMINAL_PROMPT": "0", "GIT_AUTHOR_NAME": "Contract",
            "GIT_AUTHOR_EMAIL": "contract@example.invalid",
            "GIT_COMMITTER_NAME": "Contract", "GIT_COMMITTER_EMAIL": "contract@example.invalid",
            "GIT_AUTHOR_DATE": "2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE": "2026-01-01T00:00:00Z"}


def fixture(root, name, env):
    (root / "user").mkdir()
    (root / "cwd").mkdir()
    if name == "missing-hub":
        return
    if name not in ("seeded", "wrong-branch"):
        raise ValueError(f"unknown fixture: {name}")
    shutil.copytree(ROOT / "tests/fixtures/hub", root / "hub")
    for args in (["init", "--initial-branch=main"], ["add", "."], ["commit", "-m", "contract seed"]):
        subprocess.run(["git", "-C", str(root / "hub"), *args], env=env,
                       capture_output=True, check=True, timeout=15)
    if name == "wrong-branch":
        subprocess.run(["git", "-C", str(root / "hub"), "checkout", "-b", "wrong"], env=env,
                       capture_output=True, check=True, timeout=15)


def snapshot(root):
    # Git objects are represented by log/refs/status below. All other fixture
    # files (including config/cache/journals) are compared without exclusions.
    files = {p.relative_to(root).as_posix(): encode(p.read_bytes())
             for p in sorted(root.rglob("*")) if p.is_file() and ".git" not in p.parts}
    return files


def git_state(root, env):
    if not (root / "hub/.git").is_dir():
        return None
    result = {}
    for key, args in (("status", ["status", "--porcelain=v1", "--untracked-files=all"]),
                      ("commits", ["log", "--format=%H%x00%P%x00%B%x00", "--all"]),
                      ("refs", ["show-ref"]), ("staged", ["diff", "--cached", "--binary"])):
        result[key] = encode(subprocess.run(["git", "-C", str(root / "hub"), *args],
                                            env=env, capture_output=True, check=True, timeout=15).stdout)
    return result


def execute(binary, case):
    with tempfile.TemporaryDirectory(prefix=case.get("root_prefix", "beans-contract-")) as directory, ExitStack() as resources:
        root = Path(directory)
        env = environment(root)
        env.update({k: v.replace("${FIXTURE_ROOT}", str(root)) for k, v in case.get("env", {}).items()})
        fixture(root, case["fixture"], env)
        for name, value in case.get("initial_files", {}).items():
            path = Path(name)
            if path.is_absolute() or ".." in path.parts:
                raise ValueError("initial fixture path must stay within the temporary root")
            target = root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(base64.b64decode(value, validate=True))
        if case.get("hold_lock", False):
            import fcntl
            (root / "beans/cache").mkdir(parents=True)
            lock = resources.enter_context((root / "beans/cache/hub.lock").open("wb"))
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        before = snapshot(root)
        argv = [str(binary), *case["argv"]]
        stdin = base64.b64decode(case.get("stdin_b64", ""))
        if case.get("transport", "pipe") == "pty":
            proc = capture(argv, stdin=stdin, cwd=root / "cwd", env=env,
                           timeout=case.get("timeout", 15))
        else:
            proc = subprocess.run(argv, input=stdin, cwd=root / "cwd", env=env,
                                  capture_output=True, timeout=case.get("timeout", 15))
        # Only the explicitly allocated temporary root is variable; no general
        # stripping of whitespace, IDs, ANSI, timestamps or diagnostics.
        def output(data):
            data = data.replace(os.fsencode(root), b"${FIXTURE_ROOT}")
            # Fang title-cases diagnostics whose first word is a pathname.
            # This is the exact presentation of our ASCII allocated root.
            presented_root = re.sub(r"(^|[/-])([0-9]*)([a-z])", lambda m: m[1] + m[2] + m[3].upper(), str(root))
            data = data.replace(os.fsencode(presented_root), b"${FIXTURE_ROOT_TITLE}")
            return encode(data)
        after = snapshot(root)
        changes = {p: value for p, value in after.items() if before.get(p) != value}
        removed = sorted(set(before) - set(after))
        return {"exit": proc.returncode, "stdout_b64": output(proc.stdout),
                "stderr_b64": output(proc.stderr), "changed_files": changes,
                "removed_files": removed, "git": git_state(root, env)}


def cases(census):
    result = []
    for cmd in census["commands"]:
        if cmd["retired"]:
            continue
        path = cmd["path"]
        name = "-".join(path) or "root"
        for variant, suffix in (("help", ["--help"]), ("no-args", []),
                                ("unknown-flag", ["--contract-unknown-flag"]),
                                ("trailing-one", ["./unexpected"]),
                                ("trailing-two", ["./unexpected", "./unexpected"])):
            result.append({"id": name + ":" + variant, "argv": path + suffix,
                           "stdin_b64": "", "cwd": "isolated-non-repository",
                           "fixture": "missing-hub"})
    for name, argv in (("version", ["--version"]), ("version-short", ["-v"]),
                       ("unknown-command", ["contract-unknown-command"]),
                       ("global-before", ["--json", "--project=alpha", "list", "--no-fetch"]),
                       ("global-after", ["list", "--json", "--project=alpha", "--no-fetch"]),
                       ("global-repeat", ["list", "--project=unknown", "--project=alpha", "--json", "--no-fetch"]),
                       ("bool-explicit", ["list", "--json=true", "--project=alpha", "--no-fetch=true"]),
                       ("bool-false", ["list", "--json=false", "--project=alpha", "--no-fetch"]),
                       ("unknown-bool", ["list", "--json=invalid"]),
                       ("unknown-short", ["list", "-z"]),
                       ("flag-empty", ["list", "--project=", "--all-projects", "--json", "--no-fetch"]),
                       ("negative", ["create", "--priority=-1"]),
                       ("delimiter", ["show", "--no-fetch", "--", "--json"]),
                       ("interleaved", ["show", "alpha-a1b2", "--json", "--project", "alpha", "--no-fetch"])):
        result.append({"id": name, "argv": argv, "fixture": "seeded", "stdin_b64": "", "cwd": "isolated-non-repository"})
    reads = [["list"], ["ready"], ["blocked"], ["show", "alpha-a1b2"], ["show", "alpha-ffff"],
             ["children", "alpha-a1b2"], ["dep", "tree", "alpha-a1b2"], ["dep", "cycles"],
             ["project", "list"], ["project", "show", "alpha"], ["doc", "list"],
             ["doc", "backlinks", "docs/Guide.md"], ["search", "contract"], ["memories"],
             ["request", "list"], ["request", "show", "alpha-r-c3d4"],
             ["handoff", "list"], ["handoff", "show", "alpha-e5f6"],
             ["plan", "list"], ["plan", "show", "alpha-plan-1234"],
             ["plan", "status", "alpha-plan-1234"], ["plan", "show", "alpha-plan-ffff"],
             ["list", "--closed"], ["handoff", "list", "--archived"],
             ["list", "--label", "contract", "--label", "other"],
             ["list", "--label", "contract,other"]]
    for argv in reads:
        for output in ([], ["--json"]):
            result.append({"id": "read:" + "-".join(argv) + (":json" if output else ":text"),
                           "argv": argv + ["--project", "alpha", "--no-fetch"] + output,
                           "fixture": "seeded", "stdin_b64": "", "cwd": "isolated-non-repository"})
    for variant, env in (("no-color", {"TERM": "xterm-256color"}),
                         ("color", {"TERM": "xterm-256color", "NO_COLOR": "", "CLICOLOR_FORCE": "1"})):
        for name, argv in (("help", ["--help"]), ("error", ["--contract-unknown-flag"]), ("version", ["--version"])):
            result.append({"id": "pty:" + variant + ":" + name, "argv": argv, "env": env,
                           "transport": "pty", "fixture": "missing-hub", "stdin_b64": "", "cwd": "isolated-non-repository"})
    result.append({"id": "exit-git-preflight", "argv": ["sync", "--json"],
                   "fixture": "wrong-branch", "stdin_b64": "", "cwd": "isolated-non-repository"})
    result.append({"id": "exit-lock-timeout", "argv": ["update", "alpha-a1b2", "--title", "Locked", "--no-sync", "--json", "--project", "alpha"],
                   "fixture": "seeded", "hold_lock": True, "timeout": 40,
                   "stdin_b64": "", "cwd": "isolated-non-repository"})
    custom = '[workflow]\nstatuses = ["todo", "waiting", "finished"]\ndefault = "todo"\nactive = ["todo"]\nterminal = ["finished"]\n'
    configured = {
        "custom-hub": ({"hub/beans.toml": custom}, {}),
        "custom-project": ({"hub/beans.toml": custom, "hub/projects/alpha/beans.toml": '[workflow]\ndefault = "waiting"\n'}, {}),
        "explicit-override": ({"hub/beans.toml": custom, "hub/projects/alpha/beans.toml": '[workflow]\ndefault = "waiting"\n',
                               "explicit.toml": '[workflow]\ndefault = "in_progress"\n'}, {"BN_CONFIG": "${FIXTURE_ROOT}/explicit.toml"}),
        "explicit-yaml": ({"explicit.yaml": 'workflow:\n  default: in_progress\n'}, {"BN_CONFIG": "${FIXTURE_ROOT}/explicit.yaml"}),
        "explicit-missing": ({}, {"BN_CONFIG": "${FIXTURE_ROOT}/missing.toml"}),
        "malformed-hub": ({"hub/beans.toml": '[workflow\n'}, {}),
        "unknown-workflow-key": ({"hub/beans.toml": '[workflow]\nunexpected = true\n'}, {}),
        "invalid-workflow": ({"hub/beans.toml": '[workflow]\ndefault = "not-a-status"\n'}, {}),
        "malformed-user": ({"beans/config.toml": 'actor = [\n'}, {}),
    }
    for name, (files, env) in configured.items():
        for command in (["list"], ["ready"], ["project", "show", "alpha"], ["show", "alpha-a1b2"]):
            result.append({"id": "config:" + name + ":" + "-".join(command),
                           "fixture": "seeded", "argv": command + ["--project", "alpha", "--json", "--no-fetch"],
                           "initial_files": {k: encode(v.encode()) for k, v in files.items()}, "env": env,
                           "root_prefix": "bn",
                           "stdin_b64": "", "cwd": "isolated-non-repository"})
    return result


def check(binary, corpus):
    failures = []
    for case in corpus["cases"]:
        actual = execute(binary, case)
        if actual != case["expected"]:
            fields = [key for key in actual if actual[key] != case["expected"].get(key)]
            failures.append(case["id"] + ": " + ", ".join(fields))
    return failures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "cli.json")
    parser.add_argument("--census", type=Path, default=CONTRACT / "commands.json")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if args.mode == "capture":
        authenticate_capture(binary)
        corpus = {"schema": "beans-cli-contract-v1", "baseline": json.loads((CONTRACT / "baseline.json").read_text()),
                  "cases": cases(json.loads(args.census.read_text()))}
        for case in corpus["cases"]:
            case["expected"] = execute(binary, case)
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print(f"Captured {len(corpus['cases'])} CLI cases")
    else:
        corpus = json.loads(args.corpus.read_text())
        failures = check(binary, corpus)
        if failures:
            parser.exit(1, "Contract mismatches:\n" + "\n".join(failures) + "\n")
        print(f"Passed {len(corpus['cases'])} CLI cases")


if __name__ == "__main__":
    main()
