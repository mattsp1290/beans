#!/usr/bin/env python3
"""Create a disposable source repo, code remote, and Beans hub fixture."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import hashlib
import re


def run(argv: list[str], cwd: Path | None = None, env: dict[str, str] | None = None) -> str:
    proc = subprocess.run(argv, cwd=cwd, env=env, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode:
        raise RuntimeError(f"{argv!r}: {proc.stderr}")
    return proc.stdout.strip()


def create(bn: Path, destination: Path | None = None) -> dict[str, str]:
    bn = bn.resolve()
    root = (destination or Path(tempfile.mkdtemp(prefix="bn-plan-loop-eval-"))).resolve()
    if destination:
        root.mkdir(parents=True, exist_ok=False)
    source = root / "source"; code_remote = root / "code.git"; hub_remote = root / "hub.git"; hub = root / "hub"
    for bare in (code_remote, hub_remote): run(["git", "init", "--bare", str(bare)])
    source.mkdir(); run(["git", "init", "-b", "main"], source)
    run(["git", "config", "user.name", "Eval"], source); run(["git", "config", "user.email", "eval@example.invalid"], source)
    (source / "README.md").write_text("fixture\n"); run(["git", "add", "README.md"], source); run(["git", "commit", "-m", "initial"], source)
    run(["git", "remote", "add", "origin", str(code_remote)], source); run(["git", "push", "-u", "origin", "main"], source)
    env = os.environ.copy()
    for key in ("BN_CONFIG", "BEANS_HUB", "BEANS_PROJECT", "BN_ACTOR", "CODEX_HOME"):
        env.pop(key, None)
    beans_home = root / "beans-home"
    env.update({"BEANS_HOME": str(beans_home), "BEANS_HUB": str(hub), "BN_ACTOR": "eval", "BEANS_PROJECT": "fixture", "GIT_CONFIG_GLOBAL": str(root / "gitconfig"), "GIT_TERMINAL_PROMPT": "0", "GIT_SSH_COMMAND": "ssh -o BatchMode=yes"})
    initialized_hub = json.loads(run([str(bn), "init", str(hub_remote), "--hub", str(hub), "--json"], source, env))
    if root not in Path(initialized_hub["config"]).resolve().parents:
        raise RuntimeError("bn init config escaped fixture root")
    run([str(bn), "project", "create", "fixture", "--hub", str(hub), "--json"], source, env)
    status = json.loads(run([str(bn), "status", "--hub", str(hub), "--project", "fixture", "--json", "--no-fetch"], source, env))
    if Path(status["hub"]).resolve() != hub:
        raise RuntimeError("bn resolved outside fixture hub")
    bundle = root / "bundle"
    initialized = json.loads(run([str(bn), "plan", "init", "Evaluation plan", "--output", str(bundle), "--hub", str(hub), "--project", "fixture", "--json"], source, env))
    sections = bundle / "sections"; sections.mkdir()
    app = {"version": 1, "active_users": False, "backward_compatibility_required": False, "feature_flags": "not-applicable", "confirmed_at": "2026-09-12T01:35:16Z"}
    app["confirmation_digest"] = hashlib.sha256(json.dumps(app, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    overview = "# Overview\n\n## Application context\n\n```implementation-plan\n" + json.dumps(app, sort_keys=True) + "\n```\n\nImplementation has not occurred. There are no blockers.\n"
    work1 = "# First package\n\nCreate the first bounded fixture change with observable tests.\n"
    work2 = "# Second package\n\nCreate the dependent fixture change and verify integration.\n"
    def normalized(raw: str) -> bytes:
        return ("\n".join(line.rstrip(" \t") for line in raw.replace("\r\n", "\n").split("\n")).rstrip("\n") + "\n").encode()
    execution = {"version": 1, "packages": [
        {"id": "first", "node_id": "first", "source": "sections/01-first.md", "source_digest": hashlib.sha256(normalized(work1)).hexdigest(), "prerequisites": [], "paths": ["first.txt"], "validation": ["test -f first.txt"], "acceptance": ["first.txt exists"], "exclusions": ["no deployment"]},
        {"id": "second", "node_id": "second", "source": "sections/02-second.md", "source_digest": hashlib.sha256(normalized(work2)).hexdigest(), "prerequisites": ["first"], "paths": ["second.txt"], "validation": ["test -f second.txt"], "acceptance": ["second.txt exists"], "exclusions": ["no release"]}], "references": []}
    handoff = "# Execution handoff\n\nRun first, then second. Validate each file and the final pair.\n\n```bn-execution-map\n" + json.dumps(execution, sort_keys=True) + "\n```\n"
    (sections / "00-overview.md").write_text(overview); (sections / "01-first.md").write_text(work1); (sections / "02-second.md").write_text(work2); (sections / "03-execution-handoff.md").write_text(handoff)
    manifest = (bundle / "plan.md").read_text()
    manifest = manifest.replace("status: draft", "status: ready")
    manifest = manifest.replace("updated:", "sections:\n    - sections/00-overview.md\n    - sections/01-first.md\n    - sections/02-second.md\n    - sections/03-execution-handoff.md\nupdated:")
    manifest = re.sub(r"<!-- bn:todo -->", "A disposable two-node execution used to validate the skill safely.", manifest, count=1)
    manifest = re.sub(r"<!-- bn:todo -->", "- Fixture source and isolated Beans hub.", manifest, count=1)
    manifest = re.sub(r"<!-- bn:todo -->", "1. Execute first.\n2. Execute second after first.", manifest, count=1)
    manifest = re.sub(r"<!-- bn:todo -->", "- All paths and remotes remain beneath the temporary root.", manifest, count=1)
    manifest = manifest.replace("nodes: []\nedges: []", "nodes:\n  - id: first\n    label: First fixture package\n    kind: component\n  - id: second\n    label: Second fixture package\n    kind: component\nedges:\n  - from: first\n    to: second\n    kind: precedes")
    (bundle / "plan.md").write_text(manifest)
    run([str(bn), "plan", "validate", str(bundle), "--hub", str(hub), "--project", "fixture", "--json"], source, env)
    published = json.loads(run([str(bn), "plan", "put", str(bundle), "--hub", str(hub), "--project", "fixture", "--json"], source, env))
    marker = f"<!-- bn-plan-loop:v1 plan={initialized['id']} node=first -->"
    first_issue = run([str(bn), "create", "First fixture package", "--description", marker, "--label", "plan-" + initialized["id"], "--silent", "--hub", str(hub), "--project", "fixture"], source, env)
    run([str(bn), "plan", "link", initialized["id"], "first", first_issue, "--hub", str(hub), "--project", "fixture", "--json"], source, env)
    final_status = json.loads(run([str(bn), "status", "--hub", str(hub), "--project", "fixture", "--json", "--no-fetch"], source, env))
    if final_status.get("ahead") != 0:
        raise RuntimeError("fixture hub remote is not current")
    return {"root": str(root), "source": str(source), "code_remote": str(code_remote), "hub": str(hub), "hub_remote": str(hub_remote), "bn": str(bn), "plan_id": initialized["id"], "linked_issue": first_issue, "published": str(published.get("pushed"))}


if __name__ == "__main__":
    p = argparse.ArgumentParser(); p.add_argument("--bn", required=True); p.add_argument("--output")
    a = p.parse_args(); print(json.dumps(create(Path(a.bn), Path(a.output) if a.output else None), sort_keys=True))
