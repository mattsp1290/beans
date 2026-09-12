#!/usr/bin/env python3
"""Deterministic safety helpers for the bn-plan-loop skill.

This is deliberately dependency-free. It never invokes a shell and never treats
local state as authoritative over Beans or Git.
"""

from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time
from typing import Any


APP_KEYS = {
    "version", "active_users", "backward_compatibility_required",
    "feature_flags", "confirmed_at", "confirmation_digest",
}
PACKAGE_KEYS = {
    "id", "node_id", "source", "source_digest", "prerequisites", "paths",
    "validation", "acceptance", "exclusions",
}
REFERENCE_KEYS = {"node_id", "reason"}
ID_RE = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
SHA_RE = re.compile(r"^[0-9a-f]{64}$")
PLAN_ID_RE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*-plan-[a-z0-9]+$")
FENCE_RE = re.compile(r"^```([^\s`]*)\s*$")


class ContractError(ValueError):
    pass


def canonical(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def normalized_source(data: bytes) -> bytes:
    text = data.decode("utf-8").replace("\r\n", "\n").replace("\r", "\n")
    return ("\n".join(line.rstrip(" \t") for line in text.split("\n")).rstrip("\n") + "\n").encode()


def fenced(text: str, tag: str) -> list[str]:
    found: list[str] = []
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        m = FENCE_RE.match(lines[i])
        if m and m.group(1) == tag:
            body: list[str] = []
            i += 1
            while i < len(lines) and lines[i].strip() != "```":
                body.append(lines[i])
                i += 1
            if i == len(lines):
                raise ContractError(f"unterminated {tag} fence")
            found.append("\n".join(body))
        i += 1
    return found


def restricted_yaml(text: str) -> dict[str, Any]:
    """Parse contract YAML with the repository's yaml.v3 implementation."""
    source = Path(__file__).with_name("yaml_oracle.go")
    repo_root = Path(__file__).resolve().parents[4]
    if not source.is_file() or not (repo_root / "go.mod").is_file():
        raise ContractError("canonical YAML parser is unavailable")
    proc = subprocess.run(["go", "run", str(source)], cwd=repo_root, input=text, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode: raise ContractError(proc.stderr.strip() or "YAML parser failed")
    value = json.loads(proc.stdout)
    if not isinstance(value, dict): raise ContractError("contract root must be an object")
    return value


def validate_application(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != APP_KEYS:
        raise ContractError(f"application context keys must be exactly {sorted(APP_KEYS)}")
    if value["version"] != 1:
        raise ContractError("application context version must be 1")
    for key in ("active_users", "backward_compatibility_required"):
        if type(value[key]) is not bool:
            raise ContractError(f"{key} must be a boolean")
    allowed = {"appropriate", "not-appropriate", "decide-per-pr", "not-applicable"}
    if value["feature_flags"] not in allowed:
        raise ContractError("invalid feature_flags value")
    no_consumers = not value["active_users"] and not value["backward_compatibility_required"]
    if (value["feature_flags"] == "not-applicable") != no_consumers:
        raise ContractError("feature_flags must be not-applicable exactly when both booleans are false")
    try:
        stamp = value["confirmed_at"].replace("Z", "+00:00")
        parsed = dt.datetime.fromisoformat(stamp)
        if parsed.tzinfo is None:
            raise ValueError
    except (AttributeError, ValueError):
        raise ContractError("confirmed_at must be RFC 3339 with timezone") from None
    unsigned = {k: value[k] for k in value if k != "confirmation_digest"}
    expected = sha256(canonical(unsigned))
    if value["confirmation_digest"] != expected:
        raise ContractError("application context confirmation_digest mismatch")
    return value


def validate_execution_map(value: Any, bundle: Path, graph: dict[str, Any] | None, listed_sections: set[str]) -> tuple[dict[str, Any], list[bytes]]:
    if not isinstance(value, dict) or set(value) != {"version", "packages", "references"}:
        raise ContractError("execution map keys must be exactly version, packages, references")
    if value["version"] != 1 or not isinstance(value["packages"], list) or not isinstance(value["references"], list):
        raise ContractError("invalid execution map root")
    package_ids: set[str] = set()
    node_ids: set[str] = set()
    source_bytes: list[bytes] = []
    prerequisites: dict[str, list[str]] = {}
    source_paths: set[str] = set()
    for item in value["packages"]:
        if not isinstance(item, dict) or set(item) != PACKAGE_KEYS:
            raise ContractError(f"package keys must be exactly {sorted(PACKAGE_KEYS)}")
        pid, node = item["id"], item["node_id"]
        if not isinstance(pid, str) or not ID_RE.fullmatch(pid) or pid in package_ids:
            raise ContractError(f"invalid or duplicate package id {pid!r}")
        if not isinstance(node, str) or not ID_RE.fullmatch(node) or node in node_ids:
            raise ContractError(f"invalid or duplicate node id {node!r}")
        package_ids.add(pid); node_ids.add(node)
        source = item["source"]
        if not isinstance(source, str) or not source.startswith("sections/") or source not in listed_sections or source in source_paths or ".." in Path(source).parts:
            raise ContractError(f"invalid or duplicate package source {source!r}")
        if Path(source).name in {"00-overview.md", "execution-handoff.md"} or "handoff" in Path(source).stem:
            raise ContractError("package source cannot be overview or handoff")
        source_paths.add(source)
        path = bundle / source
        if not path.is_file() or path.resolve().parent != (bundle / "sections").resolve():
            raise ContractError(f"missing package source {source}")
        data = normalized_source(path.read_bytes())
        source_bytes.append(data)
        if not isinstance(item["source_digest"], str) or not SHA_RE.fullmatch(item["source_digest"]) or sha256(data) != item["source_digest"]:
            raise ContractError(f"source digest mismatch for {source}")
        for key in ("prerequisites", "paths", "validation", "acceptance", "exclusions"):
            if not isinstance(item[key], list) or any(not isinstance(x, str) or not x.strip() for x in item[key]):
                raise ContractError(f"package {pid} field {key} must be a string list")
        for key in ("paths", "validation", "acceptance", "exclusions"):
            if not item[key]:
                raise ContractError(f"package {pid} field {key} must not be empty")
        prerequisites[pid] = item["prerequisites"]
    for item in value["references"]:
        if not isinstance(item, dict) or set(item) != REFERENCE_KEYS:
            raise ContractError(f"reference keys must be exactly {sorted(REFERENCE_KEYS)}")
        node = item["node_id"]
        if not isinstance(node, str) or not ID_RE.fullmatch(node) or node in node_ids or not isinstance(item["reason"], str) or not item["reason"].strip():
            raise ContractError(f"invalid or duplicate reference node {node!r}")
        node_ids.add(node)
    for pid, deps in prerequisites.items():
        if len(deps) != len(set(deps)) or any(dep not in package_ids or dep == pid for dep in deps):
            raise ContractError(f"invalid prerequisites for {pid}")
    visiting: set[str] = set(); done: set[str] = set()
    def visit(pid: str) -> None:
        if pid in visiting: raise ContractError("package prerequisites contain a cycle")
        if pid in done: return
        visiting.add(pid)
        for dep in prerequisites[pid]: visit(dep)
        visiting.remove(pid); done.add(pid)
    for pid in package_ids: visit(pid)
    if graph is not None:
        graph_nodes = graph.get("nodes", [])
        known = {node["id"] for node in graph_nodes if isinstance(node, dict) and isinstance(node.get("id"), str)}
        if known != node_ids:
            raise ContractError(f"graph/map node coverage differs: graph={sorted(known)} map={sorted(node_ids)}")
        node_for_package = {p["id"]: p["node_id"] for p in value["packages"]}
        desired = {(node_for_package[d], node_for_package[p]) for p, deps in prerequisites.items() for d in deps}
        actual = {(e.get("from"), e.get("to")) for e in graph.get("edges", []) if isinstance(e, dict) and e.get("kind") == "precedes" and e.get("from") in node_for_package.values() and e.get("to") in node_for_package.values()}
        if desired != actual:
            raise ContractError(f"prerequisites do not match graph precedes edges: wanted={sorted(desired)} actual={sorted(actual)}")
    return value, source_bytes


def contract(bundle: Path, graph: dict[str, Any] | None = None) -> dict[str, Any]:
    manifest_path = bundle / "plan.md"
    if not manifest_path.is_file(): raise ContractError("bundle lacks plan.md")
    manifest = manifest_path.read_text()
    listed_sections: set[str] = set()
    in_sections = False
    for line in manifest.splitlines():
        if line == "sections:": in_sections = True; continue
        if in_sections and re.fullmatch(r"\s+-\s+sections/[^\s]+\.md", line): listed_sections.add(line.split("-", 1)[1].strip()); continue
        if in_sections and line and not line.startswith((" ", "\t")): in_sections = False
    if not listed_sections: raise ContractError("manifest has no listed sections")
    sections = bundle / "sections"
    overviews = sorted(sections.glob("*overview*.md"))
    handoffs = sorted(sections.glob("*handoff*.md"))
    if len(overviews) != 1 or len(handoffs) != 1:
        raise ContractError("bundle must have exactly one overview and one handoff section")
    app_fences = fenced(overviews[0].read_text(), "implementation-plan")
    map_fences = fenced(handoffs[0].read_text(), "bn-execution-map")
    if len(app_fences) != 1 or len(map_fences) != 1:
        raise ContractError("bundle must contain exactly one application context and execution map fence")
    app = validate_application(json.loads(app_fences[0]))
    if graph is None:
        graph_fences = fenced(manifest, "bn-change-graph")
        if len(graph_fences) != 1: raise ContractError("manifest must contain exactly one change graph")
        graph = restricted_yaml(graph_fences[0])
    execution, sources = validate_execution_map(restricted_yaml(map_fences[0]), bundle, graph, listed_sections)
    semantic_graph = {
        **graph,
        "nodes": [{k: v for k, v in node.items() if k != "ref"} for node in graph.get("nodes", [])],
    }
    parts = [canonical({k: v for k, v in app.items() if k != "confirmation_digest"}), canonical(execution), canonical(semantic_graph), *sources]
    digest = hashlib.sha256()
    for part in parts:
        digest.update(len(part).to_bytes(8, "big")); digest.update(part)
    packages = []
    for item, source in zip(execution["packages"], sources):
        packages.append({**item, "package_digest": sha256(canonical(item) + b"\0" + source)})
    return {"semantic_digest": digest.hexdigest(), "application_context": app, "packages": packages, "references": execution["references"]}


def load_workflow(explicit: Path | None, project: Path | None, hub: Path | None) -> dict[str, Any]:
    source = Path(__file__).with_name("workflow_oracle.go")
    repo_root = Path(__file__).resolve().parents[4]
    if not source.is_file() or not (repo_root / "go.mod").is_file():
        raise ContractError("canonical Beans workflow oracle is unavailable")
    argv = ["go", "run", str(source), str(explicit or ""), str(project or ""), str(hub or "")]
    proc = subprocess.run(argv, cwd=repo_root, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode: raise ContractError(proc.stderr.strip() or "workflow oracle failed")
    result = json.loads(proc.stdout)
    holds = {"ready_for_review", "ready_for_validation", "ready_for_merge"}
    if not holds <= set(result["statuses"]) or holds & (set(result["active"]) | set(result["terminal"])):
        raise ContractError("required ready_for_* statuses must be holds")
    if "in_progress" not in result["statuses"] or not result["terminal"]:
        raise ContractError("workflow lacks in_progress or terminal status")
    return result


def verify_approval_logs(issue: dict[str, Any], plan_id: str, node_id: str, head: str, executor_actor: str, expected_repo: str, expected_branch: str) -> dict[str, Any]:
    marker = f"bn-plan-loop:v1 approve plan={plan_id} node={node_id} head={head}"
    logs = issue.get("log")
    if not isinstance(logs, list): raise ContractError("issue has no structured log")
    matches = [i for i, row in enumerate(logs) if isinstance(row, dict) and row.get("event") == "note — " + marker]
    if len(matches) != 1: raise ContractError("approval marker must be unique")
    index = matches[0]
    if index == 0: raise ContractError("approval marker lacks adjacent status transition")
    status, note = logs[index - 1], logs[index]
    if status.get("event") != "status ready_for_review → ready_for_validation": raise ContractError("approval status transition is missing or non-adjacent")
    if note.get("actor") == executor_actor or status.get("actor") != note.get("actor"): raise ContractError("approval actor is invalid")
    for key in ("repo", "sha", "branch"):
        if status.get(key) != note.get(key): raise ContractError(f"approval {key} context differs")
    if status.get("repo") != expected_repo or status.get("branch") != expected_branch:
        raise ContractError("approval repository or branch differs from execution identity")
    logged_sha = status.get("sha")
    if not isinstance(logged_sha, str) or len(logged_sha) < 7 or not head.startswith(logged_sha):
        raise ContractError("approval repository SHA does not identify the reviewed head")
    return {"valid": True, "status_index": index - 1, "note_index": index, "marker": marker}


def validate_preflight(value: dict[str, Any]) -> dict[str, Any]:
    required = {"active_goal", "plan_id", "lifecycle_status", "dirty", "merge_preserves_head", "project", "nodes"}
    if set(value) != required: raise ContractError(f"preflight keys must be exactly {sorted(required)}")
    if value["active_goal"] is not True: raise ContractError("active goal required: /goal $bn-plan-loop <plan-id>")
    if not isinstance(value["plan_id"], str) or not PLAN_ID_RE.fullmatch(value["plan_id"]): raise ContractError("invalid plan ID")
    if value["lifecycle_status"] != "ready": raise ContractError("plan lifecycle must be ready")
    if not isinstance(value["dirty"], list) or value["dirty"]: raise ContractError("executing checkout must be clean")
    if value["merge_preserves_head"] is not True: raise ContractError("merge policy must preserve reviewed head ancestry")
    if not isinstance(value["project"], str) or not value["project"]: raise ContractError("selected project is missing")
    if not isinstance(value["nodes"], list): raise ContractError("preflight nodes must be a list")
    for node in value["nodes"]:
        if not isinstance(node, dict) or set(node) != {"node_id", "executable", "binding", "issue_project"}: raise ContractError("invalid preflight node")
        if node["executable"]:
            if node["binding"] not in {"unlinked", "issue"}: raise ContractError(f"executable node {node['node_id']} has invalid binding")
            if node["binding"] == "issue" and node["issue_project"] != value["project"]: raise ContractError(f"executable node {node['node_id']} is cross-project")
        elif node["binding"] in {"issue", "missing_issue"}:
            raise ContractError(f"reference-only node {node['node_id']} resolves as an issue")
    return value


def classify_package_drift(previous_digest: str, current_digest: str, phase: str, material_scope_growth: bool) -> str:
    if previous_digest == current_digest: return "unchanged"
    if phase == "unstarted" and not material_scope_growth: return "reconcile_description"
    return "human_disposition"


def atomic_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".state-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as out:
            json.dump(value, out, sort_keys=True); out.write("\n"); out.flush(); os.fsync(out.fileno())
        os.replace(name, path)
    finally:
        with contextlib.suppress(FileNotFoundError): os.unlink(name)


def resolved_executable(value: str) -> str:
    found = shutil.which(value) if os.sep not in value else value
    if not found: raise ContractError(f"executable not found: {value}")
    return str(Path(found).resolve())


def state_cas(path: Path, expected: int, patch: dict[str, Any]) -> dict[str, Any]:
    current = json.loads(path.read_text()) if path.exists() else {"generation": 0}
    if current.get("generation") != expected: raise ContractError("state generation changed")
    current.update(patch); current["generation"] = expected + 1
    atomic_json(path, current)
    return current


def checkout_claim(git_dir: Path, plan_id: str, incarnation: str, owner: str, token: int) -> dict[str, Any]:
    claim_path = git_dir.resolve() / "bn-plan-loop-checkout.json"
    wanted = {"plan_id": plan_id, "incarnation": incarnation, "owner": owner, "fencing_token": token}
    if claim_path.exists():
        current = json.loads(claim_path.read_text())
        if current.get("plan_id") != plan_id or current.get("incarnation") != incarnation:
            raise ContractError(f"checkout is claimed by incompatible identity {current}")
        # A newly acquired lifetime lease may rebind the same checkout
        # incarnation after a human hold. The caller must hold that lease.
        atomic_json(claim_path, wanted)
        return wanted
    atomic_json(claim_path, wanted)
    return wanted


def checkout_release(git_dir: Path, plan_id: str, incarnation: str, owner: str, token: int) -> dict[str, Any]:
    claim_path = git_dir.resolve() / "bn-plan-loop-checkout.json"
    if not claim_path.exists(): raise ContractError("checkout has no claim to release")
    current = json.loads(claim_path.read_text())
    wanted = {"plan_id": plan_id, "incarnation": incarnation, "owner": owner, "fencing_token": token}
    if current != wanted: raise ContractError(f"checkout claim does not match release identity {current}")
    claim_path.unlink()
    return {"released": True, **wanted}


def lease_session(run_dir: Path, owner: str) -> int:
    """Hold one executor lock while serving argv-safe JSONL operations."""
    run_dir.mkdir(parents=True, exist_ok=True)
    state_path = run_dir / "state.json"
    with (run_dir / "executor.lock").open("a+") as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise ContractError("plan lease is held by another local executor") from None
        state = json.loads(state_path.read_text()) if state_path.exists() else {"generation": 0, "fencing_token": 0}
        if state.get("pending_intent") is not None:
            raise ContractError("pending mutation intent requires reconciliation before a new session")
        token = int(state.get("fencing_token", 0)) + 1
        state.update({"owner": owner, "pid": os.getpid(), "active": True, "fencing_token": token,
                      "generation": int(state.get("generation", 0)) + 1,
                      "started_at": dt.datetime.now(dt.timezone.utc).isoformat()})
        atomic_json(state_path, state)
        print(json.dumps({"ready": True, "owner": owner, "fencing_token": token}), flush=True)
        try:
            for line in sys.stdin:
                request = json.loads(line)
                op = request.get("op")
                if op == "run":
                    argv = request.get("argv")
                    if not isinstance(argv, list) or not argv or any(not isinstance(x, str) for x in argv): raise ContractError("run requires string argv")
                    result = subprocess.run(argv, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                    print(json.dumps({"returncode": result.returncode, "stdout": result.stdout, "stderr": result.stderr}), flush=True)
                elif op == "bn":
                    if state.get("pending_intent") is not None:
                        print(json.dumps({"returncode": 4, "error": "pending intent requires reconciliation"}), flush=True); continue
                    code = execute_bn_locked(state_path, state, request.get("bn"), request.get("spec"))
                    print(json.dumps({"returncode": code, "fencing_token": token}), flush=True)
                elif op == "git":
                    if state.get("pending_intent") is not None:
                        print(json.dumps({"returncode": 4, "error": "pending intent requires reconciliation"}), flush=True); continue
                    code = execute_git_locked(state_path, state, request.get("spec"))
                    print(json.dumps({"returncode": code, "fencing_token": token}), flush=True)
                elif op == "claim-checkout":
                    claimed = checkout_claim(Path(request["git_dir"]), request["plan_id"], request["incarnation"], owner, token)
                    print(json.dumps({"claimed": True, **claimed}), flush=True)
                elif op == "release-checkout":
                    released = checkout_release(Path(request["git_dir"]), request["plan_id"], request["incarnation"], owner, token)
                    print(json.dumps(released), flush=True)
                elif op == "release":
                    if state.get("pending_intent") is not None:
                        print(json.dumps({"released": False, "error": "pending intent requires reconciliation"}), flush=True); continue
                    print(json.dumps({"released": True, "fencing_token": token}), flush=True); return 0
                elif op == "abort":
                    print(json.dumps({"aborted": True, "pending_intent": state.get("pending_intent") is not None}), flush=True); return 4
                else:
                    raise ContractError(f"unknown lease-session operation {op!r}")
        finally:
            latest = json.loads(state_path.read_text())
            if latest.get("owner") == owner and latest.get("fencing_token") == token:
                latest["active"] = False; latest["generation"] = int(latest.get("generation", 0)) + 1
                atomic_json(state_path, latest)
    return 0


def matches_expectation(value: Any, expected: dict[str, Any]) -> bool:
    for dotted, wanted in expected.items():
        current = value
        for part in dotted.split("."):
            if not isinstance(current, dict) or part not in current: return False
            current = current[part]
        if isinstance(wanted, dict) and set(wanted) == {"$contains"}:
            if not isinstance(wanted["$contains"], str) or wanted["$contains"] not in json.dumps(current, sort_keys=True): return False
        elif current != wanted: return False
    return True


def run_gated_mutation(state_path: Path, state: dict[str, Any], executable: str, argv: list[str], after_started: Any = None) -> tuple[int, str]:
    """Exec only after parent and child have durably journaled the launch boundary."""
    gate_read, gate_write = os.pipe(); output_read, output_write = os.pipe()
    try:
        pid = os.fork()
    except BaseException:
        for fd in (gate_read, gate_write, output_read, output_write): os.close(fd)
        raise
    if pid == 0:
        try:
            os.close(gate_write); os.close(output_read)
            if os.read(gate_read, 1) != b"G": os._exit(125)
            child_state = json.loads(state_path.read_text())
            pending = child_state.get("pending_intent")
            if not isinstance(pending, dict) or pending.get("phase") != "started" or pending.get("child_pid") != os.getpid(): os._exit(126)
            pending["phase"] = "launched"; child_state["generation"] = int(child_state.get("generation", 0)) + 1
            atomic_json(state_path, child_state)
            os.dup2(output_write, 1)
            os.execv(executable, [executable, *argv])
        except BaseException:
            os._exit(127)
    os.close(gate_read); os.close(output_write)
    state["pending_intent"]["phase"] = "started"; state["pending_intent"]["child_pid"] = pid
    state["generation"] += 1; atomic_json(state_path, state)
    if after_started is not None: after_started()
    os.write(gate_write, b"G"); os.close(gate_write); gate_write = -1
    with os.fdopen(output_read, "r") as output: stdout = output.read()
    output_read = -1
    _, wait_status = os.waitpid(pid, 0)
    latest = json.loads(state_path.read_text()); state.clear(); state.update(latest)
    return os.waitstatus_to_exitcode(wait_status), stdout


def process_exists(pid: Any) -> bool:
    if not isinstance(pid, int) or pid <= 0: return False
    try: os.kill(pid, 0)
    except ProcessLookupError: return False
    except PermissionError: return True
    return True


def execute_bn_locked(state_path: Path, state: dict[str, Any], bn: Any, spec: Any, after_prepare: Any = None, after_started: Any = None) -> int:
    if state.get("pending_intent") is not None: raise ContractError("pending intent requires reconciliation before mutation")
    if not isinstance(bn, str) or not isinstance(spec, dict): raise ContractError("bn transaction requires executable and spec")
    if set(spec) != {"intent", "mutation", "verify", "expect", "recovery"} or not all(isinstance(spec[k], dict if k in {"intent", "expect", "recovery"} else list) for k in spec):
        raise ContractError("bn spec requires intent/expect/recovery objects and mutation/verify argv lists")
    required_identity = {"kind", "plan_id", "node_id"}
    if not required_identity <= set(spec["intent"]) or any(not isinstance(spec["intent"][key], str) or not spec["intent"][key] for key in required_identity):
        raise ContractError("intent requires nonempty kind, plan_id, and node_id")
    if not spec["expect"]: raise ContractError("bn transaction requires nonempty mutation-specific expectations")
    recovery = spec["recovery"]
    ordinary_recovery = set(recovery) == {"command", "format", "argv", "expect"} and recovery.get("command") in {"show_issue", "plan_status"} and spec["intent"].get("kind") != "create"
    marker_recovery = (set(recovery) == {"command", "format", "argv", "show_argv_suffix", "marker", "expect"}
                       and spec["intent"].get("kind") == "create" and recovery.get("command") == "issue_marker_search" and recovery.get("marker") == spec["intent"].get("marker")
                       and recovery.get("marker") == f"<!-- bn-plan-loop:v1 plan={spec['intent'].get('plan_id')} node={spec['intent'].get('node_id')} -->"
                       and recovery.get("expect") == {"matches": 1}
                       and isinstance(recovery.get("show_argv_suffix"), list) and all(isinstance(x, str) for x in recovery["show_argv_suffix"])
                       and isinstance(recovery.get("argv"), list) and bool(recovery["argv"])
                       and recovery["argv"][:6] == ["list", "--label", f"plan-{spec['intent'].get('plan_id')}", "--archived", "--limit", "0"]
                       and recovery["argv"].count("--label") == recovery["argv"].count("--archived") == recovery["argv"].count("--limit") == recovery["argv"].count("--json") == 1
                       and len(recovery["show_argv_suffix"]) == 5 and recovery["show_argv_suffix"][0] == "--hub" and bool(recovery["show_argv_suffix"][1])
                       and recovery["show_argv_suffix"][2] == "--project" and bool(recovery["show_argv_suffix"][3]) and recovery["show_argv_suffix"][4] == "--json"
                       and recovery["argv"][6:] == recovery["show_argv_suffix"])
    if not (ordinary_recovery or marker_recovery) or recovery.get("format") != "json" or not isinstance(recovery.get("argv"), list) or not recovery["argv"] or any(not isinstance(x, str) for x in recovery["argv"]) or not isinstance(recovery.get("expect"), dict) or not recovery["expect"]:
        raise ContractError("bn transaction requires a typed nonempty recovery contract")
    if any(not isinstance(x, str) for key in ("mutation", "verify") for x in spec[key]): raise ContractError("bn argv must contain strings")
    bn_path = resolved_executable(bn)
    state["pending_intent"] = {**spec["intent"], "phase": "prepared", "mutation_argv": spec["mutation"], "executable": {"kind": "bn", "path": bn_path}, "recovery": spec["recovery"], "recorded_at": dt.datetime.now(dt.timezone.utc).isoformat()}
    state["generation"] = int(state.get("generation", 0)) + 1; atomic_json(state_path, state)
    if after_prepare is not None: after_prepare()
    mutation_code, mutation_stdout = run_gated_mutation(state_path, state, bn_path, spec["mutation"], after_started)
    mutation_output = mutation_stdout.strip()
    verify_argv = [mutation_output if item == "{mutation_stdout}" else item for item in spec["verify"]]
    recovery_argv = [mutation_output if item == "{mutation_stdout}" else item for item in recovery["argv"]]
    if any(item == "" or item == "{mutation_stdout}" for item in recovery_argv):
        return mutation_code or 4
    state["pending_intent"]["recovery"]["argv"] = recovery_argv
    state["generation"] += 1; atomic_json(state_path, state)
    verified = subprocess.run([bn_path, *verify_argv], check=False, text=True, stdout=subprocess.PIPE)
    try: evidence = json.loads(verified.stdout) if verified.returncode == 0 else None
    except json.JSONDecodeError: evidence = None
    if evidence is not None and matches_expectation(evidence, spec["expect"]):
        state["pending_intent"] = None; state["generation"] += 1; state["verified_evidence_digest"] = sha256(canonical(evidence)); atomic_json(state_path, state)
        return 0
    return mutation_code or verified.returncode or 4


def execute_git_locked(state_path: Path, state: dict[str, Any], spec: Any, after_prepare: Any = None, after_started: Any = None) -> int:
    if state.get("pending_intent") is not None: raise ContractError("pending intent requires reconciliation before Git mutation")
    if not isinstance(spec, dict) or set(spec) != {"intent", "mutation", "verify", "expect", "recovery"}: raise ContractError("git transaction has invalid shape")
    for key in ("intent", "expect", "recovery"):
        if not isinstance(spec[key], dict): raise ContractError(f"git transaction {key} must be an object")
    if not spec["expect"]: raise ContractError("git transaction requires nonempty mutation-specific expectations")
    for key in ("mutation", "verify"):
        if not isinstance(spec[key], list) or any(not isinstance(x, str) for x in spec[key]): raise ContractError(f"git transaction {key} must be string argv")
    if any(not isinstance(spec["intent"].get(key), str) or not spec["intent"].get(key) for key in ("kind", "plan_id", "node_id")): raise ContractError("git intent requires kind, plan_id, and node_id")
    if set(spec["recovery"]) != {"command", "format", "argv", "expect"} or spec["recovery"]["command"] != "git_verify" or spec["recovery"]["format"] != "text" or not isinstance(spec["recovery"]["argv"], list) or not spec["recovery"]["argv"] or any(not isinstance(x, str) for x in spec["recovery"]["argv"]) or not spec["recovery"]["expect"]: raise ContractError("git recovery contract is invalid")
    git_path = resolved_executable("git")
    state["pending_intent"] = {**spec["intent"], "phase": "prepared", "mutation_argv": spec["mutation"], "executable": {"kind": "git", "path": git_path}, "recovery": spec["recovery"], "recorded_at": dt.datetime.now(dt.timezone.utc).isoformat()}
    state["generation"] = int(state.get("generation", 0)) + 1; atomic_json(state_path, state)
    if after_prepare is not None: after_prepare()
    mutation_code, _ = run_gated_mutation(state_path, state, git_path, spec["mutation"], after_started)
    verified = subprocess.run([git_path, *spec["verify"]], check=False, text=True, stdout=subprocess.PIPE)
    evidence = {"returncode": verified.returncode, "stdout": verified.stdout.strip()}
    if matches_expectation(evidence, spec["expect"]):
        state["pending_intent"] = None; state["generation"] += 1; state["verified_evidence_digest"] = sha256(canonical(evidence)); atomic_json(state_path, state)
        return 0
    return mutation_code or verified.returncode or 4


def fenced_bn(run_dir: Path, owner: str, expected_token: int, bn: str, spec_path: Path) -> int:
    spec = json.loads(spec_path.read_text())
    run_dir.mkdir(parents=True, exist_ok=True)
    with (run_dir / "executor.lock").open("a+") as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise ContractError("plan lease is held by another local executor") from None
        state_path = run_dir / "state.json"
        current = json.loads(state_path.read_text()) if state_path.exists() else {"generation": 0, "fencing_token": 0}
        if int(current.get("fencing_token", 0)) != expected_token:
            raise ContractError("stale fencing token; reconcile before mutation")
        if current.get("pending_intent") is not None:
            raise ContractError("pending mutation intent requires reconcile-bn before mutation")
        current.update({"owner": owner, "pid": os.getpid(), "active": True, "fencing_token": expected_token + 1,
                        "generation": int(current.get("generation", 0)) + 1,
                        })
        atomic_json(state_path, current)
        try:
            return execute_bn_locked(state_path, current, bn, spec)
        finally:
            current["active"] = False; current["generation"] = int(current.get("generation", 0)) + 1
            atomic_json(state_path, current)


def reconcile_bn(run_dir: Path, owner: str, expected_token: int, bn: str, spec_path: Path) -> int:
    spec = json.loads(spec_path.read_text())
    if set(spec) != {"intent"} or not isinstance(spec["intent"], dict):
        raise ContractError("reconcile-bn accepts only the persisted intent identity")
    run_dir.mkdir(parents=True, exist_ok=True); state_path = run_dir / "state.json"
    with (run_dir / "executor.lock").open("a+") as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise ContractError("plan lease is held by another local executor") from None
        state = json.loads(state_path.read_text()) if state_path.exists() else {}
        if int(state.get("fencing_token", -1)) != expected_token: raise ContractError("stale fencing token during reconciliation")
        if state.get("pending_intent") is None: raise ContractError("there is no pending intent to reconcile")
        persisted = state["pending_intent"]
        for key in ("kind", "plan_id", "node_id", "marker", "issue_id"):
            if key in persisted and spec["intent"].get(key) != persisted[key]:
                raise ContractError(f"reconciliation identity differs for {key}")
        recovery = persisted.get("recovery")
        if not isinstance(recovery, dict): raise ContractError("pending intent lacks a recovery contract")
        executable = persisted.get("executable")
        expected_kind = "git" if recovery.get("command") == "git_verify" else "bn"
        if not isinstance(executable, dict) or executable.get("kind") != expected_kind or executable.get("path") != resolved_executable("git" if expected_kind == "git" else bn):
            raise ContractError("reconciliation executable differs from the persisted authority")
        if expected_kind == "git" and resolved_executable(bn) != resolved_executable("git"):
            raise ContractError("Git reconciliation requires the Git executable")
        verifier = resolved_executable("git") if expected_kind == "git" else executable["path"]
        if persisted.get("phase") == "prepared":
            if not isinstance(persisted.get("mutation_argv"), list) or any(not isinstance(x, str) for x in persisted["mutation_argv"]):
                raise ContractError("prepared intent lacks exact mutation argv")
            state.update({"owner": owner, "pid": os.getpid(), "active": False, "fencing_token": expected_token + 1,
                          "generation": int(state.get("generation", 0)) + 1, "pending_intent": None, "recovered_unstarted": True})
            atomic_json(state_path, state)
            return 0
        if persisted.get("phase") == "started":
            if process_exists(persisted.get("child_pid")): raise ContractError("gated mutation child is still alive")
            state.update({"owner": owner, "pid": os.getpid(), "active": False, "fencing_token": expected_token + 1,
                          "generation": int(state.get("generation", 0)) + 1, "pending_intent": None, "recovered_unlaunched": True})
            atomic_json(state_path, state)
            return 0
        if persisted.get("phase") != "launched": raise ContractError("pending intent has an invalid transaction phase")
        ordinary_shape = set(recovery) == {"command", "format", "argv", "expect"}
        marker_shape = set(recovery) == {"command", "format", "argv", "show_argv_suffix", "marker", "expect"}
        if not (ordinary_shape or marker_shape) or not isinstance(recovery.get("argv"), list) or not recovery["argv"] or any(not isinstance(x, str) for x in recovery["argv"]):
            raise ContractError("pending intent lacks an exact recovery command")
        required_prefix = ["show"] if recovery.get("command") == "show_issue" else (["plan", "status"] if recovery.get("command") == "plan_status" else (["list"] if recovery.get("command") == "issue_marker_search" else ([] if recovery.get("command") == "git_verify" else None)))
        if required_prefix is None or recovery["argv"][:len(required_prefix)] != required_prefix:
            raise ContractError("persisted recovery command has the wrong operation type")
        state.update({"owner": owner, "pid": os.getpid(), "active": True, "fencing_token": expected_token + 1, "generation": int(state.get("generation", 0)) + 1})
        atomic_json(state_path, state)
        verified = subprocess.run([verifier, *recovery["argv"]], check=False, text=True, stdout=subprocess.PIPE)
        evidence: Any
        if recovery.get("command") == "issue_marker_search" and verified.returncode == 0:
            try: rows = json.loads(verified.stdout)
            except json.JSONDecodeError: rows = None
            matches: list[str] = []
            if isinstance(rows, list) and isinstance(recovery.get("show_argv_suffix"), list) and isinstance(recovery.get("marker"), str):
                for row in rows:
                    if not isinstance(row, dict) or not isinstance(row.get("id"), str): continue
                    shown = subprocess.run([verifier, "show", row["id"], *recovery["show_argv_suffix"]], check=False, text=True, stdout=subprocess.PIPE)
                    try: detail = json.loads(shown.stdout) if shown.returncode == 0 else None
                    except json.JSONDecodeError: detail = None
                    if isinstance(detail, dict) and recovery["marker"] in detail.get("description", ""): matches.append(row["id"])
            evidence = {"matches": len(matches), "issue_id": matches[0] if len(matches) == 1 else ""}
        elif recovery.get("format") == "text":
            evidence = {"returncode": verified.returncode, "stdout": verified.stdout.strip()}
        else:
            try: evidence = json.loads(verified.stdout) if verified.returncode == 0 else None
            except json.JSONDecodeError: evidence = None
        if evidence is None or not matches_expectation(evidence, recovery["expect"]):
            state["active"] = False; state["generation"] += 1; atomic_json(state_path, state)
            return verified.returncode or 4
        state["pending_intent"] = None; state["active"] = False; state["generation"] += 1; state["verified_evidence_digest"] = sha256(canonical(evidence)); atomic_json(state_path, state)
        return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    val = sub.add_parser("validate-contract"); val.add_argument("--bundle", required=True); val.add_argument("--graph-json")
    wf = sub.add_parser("workflow"); wf.add_argument("--explicit"); wf.add_argument("--project"); wf.add_argument("--hub")
    approval = sub.add_parser("verify-approval"); approval.add_argument("--issue-json", required=True); approval.add_argument("--plan", required=True); approval.add_argument("--node", required=True); approval.add_argument("--head", required=True); approval.add_argument("--executor-actor", required=True); approval.add_argument("--repo", required=True); approval.add_argument("--branch", required=True)
    preflight = sub.add_parser("validate-preflight"); preflight.add_argument("--input", required=True)
    session = sub.add_parser("lease-session"); session.add_argument("--run-dir", required=True); session.add_argument("--owner", required=True)
    fenced = sub.add_parser("fenced-bn"); fenced.add_argument("--run-dir", required=True); fenced.add_argument("--owner", required=True); fenced.add_argument("--expected-token", required=True, type=int); fenced.add_argument("--bn", required=True); fenced.add_argument("--spec", required=True)
    reconcile = sub.add_parser("reconcile-bn"); reconcile.add_argument("--run-dir", required=True); reconcile.add_argument("--owner", required=True); reconcile.add_argument("--expected-token", required=True, type=int); reconcile.add_argument("--bn", required=True); reconcile.add_argument("--spec", required=True)
    args = parser.parse_args()
    if args.command == "validate-contract":
        graph = json.loads(Path(args.graph_json).read_text()) if args.graph_json else None
        print(json.dumps(contract(Path(args.bundle), graph), indent=2, sort_keys=True)); return 0
    if args.command == "workflow":
        print(json.dumps(load_workflow(Path(args.explicit) if args.explicit else None, Path(args.project) if args.project else None, Path(args.hub) if args.hub else None), sort_keys=True)); return 0
    if args.command == "verify-approval":
        print(json.dumps(verify_approval_logs(json.loads(Path(args.issue_json).read_text()), args.plan, args.node, args.head, args.executor_actor, args.repo, args.branch), sort_keys=True)); return 0
    if args.command == "validate-preflight":
        print(json.dumps(validate_preflight(json.loads(Path(args.input).read_text())), sort_keys=True)); return 0
    if args.command == "lease-session":
        return lease_session(Path(args.run_dir), args.owner)
    if args.command == "fenced-bn":
        return fenced_bn(Path(args.run_dir), args.owner, args.expected_token, args.bn, Path(args.spec))
    if args.command == "reconcile-bn":
        return reconcile_bn(Path(args.run_dir), args.owner, args.expected_token, args.bn, Path(args.spec))
    return 2


if __name__ == "__main__":
    try: raise SystemExit(main())
    except (ContractError, json.JSONDecodeError, OSError) as exc:
        print(f"bn-plan-loop: {exc}", file=sys.stderr); raise SystemExit(2)
