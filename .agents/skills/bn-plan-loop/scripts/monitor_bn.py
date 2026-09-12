#!/usr/bin/env python3
"""Read-only, fingerprinted hold monitor for bn-plan-loop."""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from typing import Any


def run_json(argv: list[str]) -> Any:
    proc = subprocess.run(argv, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode:
        raise RuntimeError(f"read failed ({proc.returncode}): {proc.stderr.strip()}")
    return json.loads(proc.stdout)


def snapshot(bn: str, common: list[str], plan: str, issue: str, remote: str | None, remote_branch: str | None, remote_timeout: float = 30) -> dict[str, Any]:
    hub = run_json([bn, *common, "status", "--json"])
    # Fetch time and explanatory resolution text can change without workflow
    # state changing. Keep only evidence relevant to hub health and identity.
    normalized_hub = {key: hub.get(key) for key in ("hub", "remote", "branch", "ahead", "behind", "dirty", "project", "project_dir")}
    result = {
        "issue": run_json([bn, *common, "show", issue, "--json"]),
        "plan": run_json([bn, *common, "plan", "status", plan, "--json"]),
        "hub": normalized_hub,
    }
    if bool(remote) != bool(remote_branch): raise RuntimeError("--remote and --remote-branch must be supplied together")
    if remote:
        assert remote_branch is not None
        git_env = os.environ.copy(); git_env["GIT_TERMINAL_PROMPT"] = "0"; git_env.setdefault("GIT_SSH_COMMAND", "ssh -o BatchMode=yes")
        try:
            proc = subprocess.run(["git", "ls-remote", "--exit-code", remote, "refs/heads/" + remote_branch], check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=git_env, timeout=remote_timeout)
        except subprocess.TimeoutExpired as exc:
            raise RuntimeError(f"remote ref read timed out after {remote_timeout:g}s") from exc
        if proc.returncode: raise RuntimeError(f"remote ref read failed: {proc.stderr.strip()}")
        result["remote_head"] = proc.stdout.split()[0]
    return result


def fingerprint(value: Any) -> str:
    raw = json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    return hashlib.sha256(raw).hexdigest()


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("mode", choices=["snapshot", "monitor"])
    p.add_argument("--bn", required=True); p.add_argument("--plan", required=True); p.add_argument("--issue", required=True)
    p.add_argument("--lock", required=True); p.add_argument("--expected"); p.add_argument("--remote"); p.add_argument("--remote-branch")
    p.add_argument("--interval", type=float, default=300); p.add_argument("--max-ticks", type=int, default=0); p.add_argument("--remote-timeout", type=float, default=30)
    p.add_argument("--bn-arg", action="append", default=[])
    args = p.parse_args()
    if args.mode == "snapshot":
        current = snapshot(args.bn, args.bn_arg, args.plan, args.issue, args.remote, args.remote_branch, args.remote_timeout)
        current_fp = fingerprint(current)
        print(json.dumps({"fingerprint": current_fp, "snapshot": current}, sort_keys=True)); return 0
    if not args.expected: p.error("monitor requires --expected")
    lock_path = Path(args.lock); lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open("a+") as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print("bn-plan-loop: monitor already active", file=sys.stderr); return 3
        failures = 0
        while True:
            try:
                current = snapshot(args.bn, args.bn_arg, args.plan, args.issue, args.remote, args.remote_branch, args.remote_timeout)
                current_fp = fingerprint(current)
                break
            except (RuntimeError, json.JSONDecodeError) as exc:
                failures += 1
                if failures >= 3:
                    print(f"bn-plan-loop: monitor failed for three ticks: {exc}", file=sys.stderr); return 4
                time.sleep(args.interval)
        if current_fp != args.expected:
            print(json.dumps({"changed": True, "fingerprint": current_fp, "snapshot": current}, sort_keys=True)); return 0
        failures = ticks = 0
        while args.max_ticks == 0 or ticks < args.max_ticks:
            time.sleep(args.interval); ticks += 1
            try:
                current = snapshot(args.bn, args.bn_arg, args.plan, args.issue, args.remote, args.remote_branch, args.remote_timeout)
                current_fp = fingerprint(current); failures = 0
            except (RuntimeError, json.JSONDecodeError) as exc:
                failures += 1
                if failures >= 3:
                    print(f"bn-plan-loop: monitor failed for three ticks: {exc}", file=sys.stderr); return 4
                continue
            if current_fp != args.expected:
                print(json.dumps({"changed": True, "fingerprint": current_fp, "snapshot": current}, sort_keys=True)); return 0
        print(json.dumps({"changed": False, "fingerprint": current_fp}, sort_keys=True)); return 5


if __name__ == "__main__":
    try: raise SystemExit(main())
    except (RuntimeError, json.JSONDecodeError, OSError) as exc:
        print(f"bn-plan-loop: {exc}", file=sys.stderr); raise SystemExit(2)
