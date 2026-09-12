#!/usr/bin/env python3
"""Executable disposable scenarios using the real repository-built bn."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
from typing import Any

from fixture_repo import create
from assert_trace import assert_trace

HELPER = Path(__file__).parents[1] / "scripts" / "bn_plan_loop.py"


def command(argv: list[str], cwd: Path, env: dict[str, str], json_output: bool = False, provenance: str = "executor") -> Any:
    trace = env.get("BN_PLAN_LOOP_SCENARIO_TRACE")
    if trace:
        with Path(trace).open("a") as out: out.write(json.dumps({"provenance": provenance, "argv": argv}) + "\n")
    proc = subprocess.run(argv, cwd=cwd, env=env, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode:
        raise AssertionError(f"command failed {argv!r}: {proc.stderr}")
    return json.loads(proc.stdout) if json_output else proc.stdout.strip()


class Scenario:
    def __init__(self, bn: Path):
        self.info = create(bn)
        self.root = Path(self.info["root"]); self.repo = Path(self.info["source"]); self.hub = Path(self.info["hub"])
        self.bn = str(bn.resolve()); self.plan = self.info["plan_id"]; self.first = self.info["linked_issue"]
        self.trace = self.root / "trace.jsonl"
        self.env = os.environ.copy()
        for key in ("BN_CONFIG", "BEANS_HUB", "BEANS_PROJECT", "BN_ACTOR", "CODEX_HOME"):
            self.env.pop(key, None)
        self.env.update({"BEANS_HOME": str(self.root / "beans-home"), "BEANS_HUB": str(self.hub), "BEANS_PROJECT": "fixture", "BN_ACTOR": "executor", "GIT_CONFIG_GLOBAL": str(self.root / "gitconfig"), "GIT_TERMINAL_PROMPT": "0", "GIT_SSH_COMMAND": "ssh -o BatchMode=yes", "BN_PLAN_LOOP_SCENARIO_TRACE": str(self.trace)})
        self.session_number = 0; self.session: subprocess.Popen[str] | None = None
        self.run_dir = self.root / "run"; self.incarnation = str(self.repo.resolve())
        self.resume_session()

    def record(self, argv: list[str]) -> None:
        with self.trace.open("a") as out: out.write(json.dumps({"provenance": "executor", "argv": argv, "via": "lease-session"}) + "\n")

    def request(self, value: dict[str, Any], allow_failure: bool = False) -> dict[str, Any]:
        assert self.session and self.session.stdin and self.session.stdout
        self.session.stdin.write(json.dumps(value) + "\n"); self.session.stdin.flush()
        response = json.loads(self.session.stdout.readline())
        if not allow_failure and (response.get("returncode", 0) != 0 or response.get("error")):
            raise AssertionError(f"session request failed: {value!r}: {response!r}")
        return response

    def resume_session(self) -> None:
        self.session_number += 1
        self.session = subprocess.Popen([os.environ.get("PYTHON", "python3"), str(HELPER), "lease-session", "--run-dir", str(self.run_dir), "--owner", f"scenario-{self.session_number}"], cwd=self.repo, env=self.env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        assert self.session.stdout
        ready = json.loads(self.session.stdout.readline()); assert ready["ready"]
        git_dir_value = Path(self.session_run(["git", "rev-parse", "--git-common-dir"]))
        git_dir = (self.repo / git_dir_value).resolve() if not git_dir_value.is_absolute() else git_dir_value.resolve()
        self.request({"op": "claim-checkout", "git_dir": str(git_dir), "plan_id": self.plan, "incarnation": self.incarnation})

    def pause_session(self) -> None:
        if not self.session: return
        process = self.session; self.request({"op": "release"}); process.wait(timeout=5)
        for stream in (process.stdin, process.stdout, process.stderr):
            if stream: stream.close()
        self.session = None

    def session_run(self, argv: list[str]) -> str:
        self.record(argv); result = self.request({"op": "run", "argv": argv})
        if result["returncode"]: raise AssertionError(f"read command failed: {argv!r}: {result['stderr']}")
        return str(result["stdout"]).strip()

    def session_probe(self, argv: list[str]) -> dict[str, Any]:
        self.record(argv); return self.request({"op": "run", "argv": argv}, allow_failure=True)

    def bnargs(self, args: list[str], json_output: bool = False) -> list[str]:
        result = [*args, "--hub", str(self.hub), "--project", "fixture"]
        if json_output: result.append("--json")
        return result

    def bn_tx(self, kind: str, node: str, mutation: list[str], verify: list[str], expect: dict[str, Any]) -> None:
        mutation_argv = self.bnargs(mutation); verify_argv = self.bnargs(verify, True)
        self.record([self.bn, *mutation_argv])
        intent = {"kind": kind, "plan_id": self.plan, "node_id": node}
        recovery: dict[str, Any] = {"command": "plan_status" if verify[:2] == ["plan", "status"] else "show_issue", "format": "json", "argv": verify_argv, "expect": expect}
        if kind == "create":
            marker = next(value for value in mutation if "bn-plan-loop:v1 plan=" in value)
            intent["marker"] = marker
            recovery = {"command": "issue_marker_search", "format": "json", "argv": self.bnargs(["list", "--label", "plan-" + self.plan, "--archived", "--limit", "0"], True),
                        "show_argv_suffix": self.bnargs([], True), "marker": marker, "expect": {"matches": 1}}
        spec = {"intent": intent, "mutation": mutation_argv, "verify": verify_argv, "expect": expect, "recovery": recovery}
        self.request({"op": "bn", "bn": self.bn, "spec": spec})

    def git_tx(self, kind: str, node: str, mutation: list[str], verify: list[str], expect: dict[str, Any]) -> None:
        self.record(["git", *mutation])
        spec = {"intent": {"kind": kind, "plan_id": self.plan, "node_id": node}, "mutation": mutation, "verify": verify, "expect": expect,
                "recovery": {"command": "git_verify", "format": "text", "argv": verify, "expect": expect}}
        self.request({"op": "git", "spec": spec})

    def cleanup(self) -> None:
        self.pause_session()
        if not self.root.name.startswith("bn-plan-loop-eval-") or not self.root.is_dir():
            raise AssertionError(f"refusing unsafe fixture cleanup: {self.root}")
        shutil.rmtree(self.root)

    def bnrun(self, *args: str, actor: str | None = None, json_output: bool = False) -> Any:
        if actor is None and args and args[0] in {"create", "update", "close", "note", "dep", "sync"}:
            raise AssertionError(f"executor mutation bypassed session: {args!r}")
        if actor is None and args[:2] in {("plan", "link"), ("plan", "put")}:
            raise AssertionError(f"executor mutation bypassed session: {args!r}")
        argv = [self.bn, *args, "--hub", str(self.hub), "--project", "fixture"]
        if actor: argv.extend(["--actor", actor])
        if json_output: argv.append("--json")
        if actor:
            return command(argv, self.repo, self.env, json_output, "human")
        output = self.session_run(argv)
        return json.loads(output) if json_output else output

    def assert_safe_trace(self) -> None:
        assert_trace(self.trace)

    def issue(self, issue_id: str) -> dict:
        return self.bnrun("show", issue_id, json_output=True)  # type: ignore[return-value]

    def materialize_second(self) -> str:
        marker = f"<!-- bn-plan-loop:v1 plan={self.plan} node=second -->"
        self.bn_tx("create", "second", ["create", "Second fixture package", "--description", marker, "--label", "plan-" + self.plan, "--silent"], ["show", "{mutation_stdout}"], {"description": {"$contains": marker}})
        rows = self.bnrun("list", "--label", "plan-" + self.plan, "--archived", "--limit", "0", json_output=True)
        second = next(row["id"] for row in rows if marker in self.issue(row["id"]).get("description", ""))
        self.bn_tx("link", "second", ["plan", "link", self.plan, "second", second], ["plan", "status", self.plan], {"counts.distinct_issues": 2})
        self.bn_tx("dependency", "second", ["dep", "add", second, self.first], ["show", second], {"blocked_by": [self.first]})
        detail = self.issue(second)
        assert self.first in detail["blocked_by"]
        return second

    def deliver(self, issue_id: str, node: str, filename: str, sequence: int) -> str:
        remote_head = self.session_run(["git", "ls-remote", "origin", "refs/heads/main"]).split()[0]
        self.git_tx("fetch", node, ["fetch", "origin", "main"], ["rev-parse", "origin/main"], {"returncode": 0, "stdout": remote_head})
        branch = f"bn-plan/{self.plan}/{sequence}-{node}"
        self.git_tx("branch-create", node, ["checkout", "-b", branch, "origin/main"], ["branch", "--show-current"], {"returncode": 0, "stdout": branch})
        self.bn_tx("claim", node, ["update", issue_id, "--claim"], ["show", issue_id], {"status": "in_progress"})
        (self.repo / filename).write_text(node + "\n")
        self.git_tx("stage", node, ["add", filename], ["diff", "--cached", "--name-only"], {"returncode": 0, "stdout": filename})
        self.git_tx("commit", node, ["commit", "-m", node], ["log", "-1", "--pretty=%s"], {"returncode": 0, "stdout": node})
        head = self.session_run(["git", "rev-parse", "HEAD"])
        remote_ref = "refs/heads/" + branch
        self.git_tx("push-feature", node, ["push", "origin", "HEAD:" + remote_ref], ["ls-remote", "origin", remote_ref], {"returncode": 0, "stdout": head + "\t" + remote_ref})
        review_note = f"bn-plan-loop:v1 phase=review plan={self.plan} node={node} head={head}"
        self.bn_tx("review-note", node, ["note", issue_id, review_note], ["show", issue_id], {"log": {"$contains": review_note}})
        self.bn_tx("review-hold", node, ["update", issue_id, "--status", "ready_for_review"], ["show", issue_id], {"status": "ready_for_review"})
        self.pause_session()
        approval = f"bn-plan-loop:v1 approve plan={self.plan} node={node} head={head}"
        self.bnrun("update", issue_id, "--status", "ready_for_validation", "--note", approval, actor="human")
        self.resume_session()
        detail = self.issue(issue_id); events = [row["event"] for row in detail["log"]]
        assert any("status ready_for_review → ready_for_validation" in event for event in events)
        assert sum(approval in event for event in events) == 1
        self.bn_tx("merge-hold", node, ["update", issue_id, "--status", "ready_for_merge"], ["show", issue_id], {"status": "ready_for_merge"})
        self.pause_session()
        # Simulated human fast-forward delivery and acceptance in local-only fixtures.
        command(["git", "push", "origin", "HEAD:main"], self.repo, self.env, provenance="human")
        self.bnrun("close", issue_id, "-r", "fixture merge accepted", actor="human")
        self.resume_session()
        self.git_tx("fetch-delivery", node, ["fetch", "origin", "main"], ["rev-parse", "origin/main"], {"returncode": 0, "stdout": head})
        self.session_run(["git", "merge-base", "--is-ancestor", head, "origin/main"])
        return head

    def complete_plan(self) -> None:
        before = self.bnrun("plan", "status", self.plan, json_output=True)
        assert before["execution_state"] == "done" and before["lifecycle_status"] == "ready" and before["lifecycle_mismatch"]
        output = self.root / "completion"
        self.bnrun("plan", "get", self.plan, "--output", str(output), json_output=True)
        manifest = output / "plan.md"; text = manifest.read_text(); assert text.count("status: ready") == 1
        manifest.write_text(text.replace("status: ready", "status: complete", 1))
        self.bnrun("plan", "validate", str(output), json_output=True)
        self.bn_tx("complete-plan", "__plan__", ["plan", "put", str(output)], ["plan", "status", self.plan], {"lifecycle_status": "complete", "execution_state": "done"})
        after = self.bnrun("plan", "status", self.plan, json_output=True)
        assert after["execution_state"] == "done" and after["lifecycle_status"] == "complete" and not after["lifecycle_mismatch"]
        assert self.bnrun("status", "--no-fetch", json_output=True)["ahead"] == 0


def normal(bn: Path) -> dict[str, str]:
    s = Scenario(bn)
    try:
        second = s.materialize_second(); first_head = s.deliver(s.first, "first", "first.txt", 1)
        status = s.bnrun("plan", "status", s.plan, json_output=True)
        second_node = next(node for node in status["nodes"] if node["node_id"] == "second")
        assert second_node["work_state"] == "runnable"
        second_head = s.deliver(second, "second", "second.txt", 2); s.complete_plan()
        s.assert_safe_trace(); return {"scenario": "normal-two-node", "plan": s.plan, "first_head": first_head, "second_head": second_head}
    finally: s.cleanup()


def materialization_resume(bn: Path) -> dict[str, str]:
    s = Scenario(bn)
    try:
        marker = f"<!-- bn-plan-loop:v1 plan={s.plan} node=second -->"
        mutation = s.bnargs(["create", "Second fixture package", "--description", marker, "--label", "plan-" + s.plan, "--silent"])
        verify = s.bnargs(["show", "{mutation_stdout}"], True)
        intent = {"kind": "create", "plan_id": s.plan, "node_id": "second", "marker": marker}
        spec = {"intent": intent, "mutation": mutation, "verify": verify, "expect": {"description": "forced interruption"},
                "recovery": {"command": "issue_marker_search", "format": "json", "argv": s.bnargs(["list", "--label", "plan-" + s.plan, "--archived", "--limit", "0"], True),
                             "show_argv_suffix": s.bnargs([], True), "marker": marker, "expect": {"matches": 1}}}
        s.record([s.bn, *mutation]); failed = s.request({"op": "bn", "bn": s.bn, "spec": spec}, allow_failure=True); assert failed["returncode"] != 0
        process = s.session; s.request({"op": "abort"}, allow_failure=True); assert process; process.wait(timeout=5)
        for stream in (process.stdin, process.stdout, process.stderr):
            if stream: stream.close()
        s.session = None
        recovery = s.root / "recovery.json"; recovery.write_text(json.dumps({"intent": intent}))
        state = json.loads((s.run_dir / "state.json").read_text()); token = state["fencing_token"]
        command([os.environ.get("PYTHON", "python3"), str(HELPER), "reconcile-bn", "--run-dir", str(s.run_dir), "--owner", "recovery", "--expected-token", str(token), "--bn", s.bn, "--spec", str(recovery)], s.repo, s.env)
        s.resume_session()
        rows = s.bnrun("list", "--label", "plan-" + s.plan, "--archived", "--limit", "0", json_output=True)
        matches = [row["id"] for row in rows if marker in s.issue(row["id"]).get("description", "")]
        assert len(matches) == 1; orphan = matches[0]
        s.bn_tx("link", "second", ["plan", "link", s.plan, "second", orphan], ["plan", "status", s.plan], {"counts.distinct_issues": 2})
        assert s.bnrun("plan", "status", s.plan, json_output=True)["counts"]["distinct_issues"] == 2
        s.assert_safe_trace(); result = {"scenario": "materialization-resume", "plan": s.plan, "orphan": orphan}
    finally: s.cleanup()
    terminal = Scenario(bn)
    try:
        terminal_marker = f"<!-- bn-plan-loop:v1 plan={terminal.plan} node=second -->"
        terminal.bn_tx("create", "second", ["create", "Terminal orphan", "--description", terminal_marker, "--label", "plan-" + terminal.plan, "--silent"], ["show", "{mutation_stdout}"], {"description": {"$contains": terminal_marker}})
        rows = terminal.bnrun("list", "--label", "plan-" + terminal.plan, "--archived", "--limit", "0", json_output=True)
        terminal_orphan = next(row["id"] for row in rows if terminal_marker in terminal.issue(row["id"]).get("description", ""))
        terminal.pause_session(); terminal.bnrun("close", terminal_orphan, "-r", "interrupted fixture", actor="human"); terminal.resume_session()
        rows = terminal.bnrun("list", "--label", "plan-" + terminal.plan, "--archived", "--limit", "0", json_output=True)
        matches = [row["id"] for row in rows if terminal_marker in terminal.issue(row["id"]).get("description", "")]
        assert matches == [terminal_orphan]
        assert sum(1 for line in terminal.trace.read_text().splitlines() if "create" in json.loads(line)["argv"]) == 1
        terminal.assert_safe_trace(); result["terminal_orphan"] = terminal_orphan
    finally: terminal.cleanup()
    return result


def false_terminal(bn: Path) -> dict[str, str]:
    s = Scenario(bn)
    try:
        second = s.materialize_second(); remote_head = s.session_run(["git", "ls-remote", "origin", "refs/heads/main"]).split()[0]
        s.git_tx("fetch", "first", ["fetch", "origin", "main"], ["rev-parse", "origin/main"], {"returncode": 0, "stdout": remote_head})
        s.git_tx("branch-create", "first", ["checkout", "-b", "bn-plan/false", "origin/main"], ["branch", "--show-current"], {"returncode": 0, "stdout": "bn-plan/false"})
        s.bn_tx("claim", "first", ["update", s.first, "--claim"], ["show", s.first], {"status": "in_progress"})
        (s.repo / "unmerged.txt").write_text("unmerged\n")
        s.git_tx("stage", "first", ["add", "unmerged.txt"], ["diff", "--cached", "--name-only"], {"returncode": 0, "stdout": "unmerged.txt"})
        s.git_tx("commit", "first", ["commit", "-m", "unmerged"], ["log", "-1", "--pretty=%s"], {"returncode": 0, "stdout": "unmerged"})
        head = s.session_run(["git", "rev-parse", "HEAD"]); s.pause_session(); s.bnrun("close", s.first, "-r", "premature", actor="human"); s.resume_session()
        ancestry = s.session_probe(["git", "merge-base", "--is-ancestor", head, "origin/main"])["returncode"]
        assert ancestry != 0 and s.issue(second)["status"] == "open"
        base = s.session_run(["git", "rev-parse", "origin/main"]); tree = s.session_run(["git", "rev-parse", head + "^{tree}"])
        s.pause_session()
        squash = str(command(["git", "commit-tree", tree, "-p", base, "-m", "squash equivalent"], s.repo, s.env, provenance="human"))
        command(["git", "push", "origin", squash + ":main"], s.repo, s.env, provenance="human"); s.resume_session()
        s.git_tx("fetch-squash", "first", ["fetch", "origin", "main"], ["rev-parse", "origin/main"], {"returncode": 0, "stdout": squash})
        assert s.session_probe(["git", "merge-base", "--is-ancestor", head, "origin/main"])["returncode"] != 0
        s.pause_session()
        merge = str(command(["git", "commit-tree", tree, "-p", squash, "-p", head, "-m", "preserve reviewed head"], s.repo, s.env, provenance="human"))
        command(["git", "push", "origin", merge + ":main"], s.repo, s.env, provenance="human"); s.resume_session()
        s.git_tx("fetch-merge", "first", ["fetch", "origin", "main"], ["rev-parse", "origin/main"], {"returncode": 0, "stdout": merge})
        s.session_run(["git", "merge-base", "--is-ancestor", head, "origin/main"])
        s.bn_tx("claim", "second", ["update", second, "--claim"], ["show", second], {"status": "in_progress"}); assert s.issue(second)["status"] == "in_progress"
        s.assert_safe_trace(); return {"scenario": "false-terminal", "plan": s.plan, "rejected_head": head}
    finally: s.cleanup()


def feedback_and_holds(bn: Path) -> dict[str, str]:
    s = Scenario(bn)
    try:
        remote_head = s.session_run(["git", "ls-remote", "origin", "refs/heads/main"]).split()[0]
        s.git_tx("fetch", "first", ["fetch", "origin", "main"], ["rev-parse", "origin/main"], {"returncode": 0, "stdout": remote_head})
        s.git_tx("branch-create", "first", ["checkout", "-b", "bn-plan/feedback", "origin/main"], ["branch", "--show-current"], {"returncode": 0, "stdout": "bn-plan/feedback"})
        s.bn_tx("claim", "first", ["update", s.first, "--claim"], ["show", s.first], {"status": "in_progress"})
        (s.repo / "feedback.txt").write_text("v1\n"); s.git_tx("stage", "first", ["add", "feedback.txt"], ["diff", "--cached", "--name-only"], {"returncode": 0, "stdout": "feedback.txt"})
        s.git_tx("commit", "first", ["commit", "-m", "v1"], ["log", "-1", "--pretty=%s"], {"returncode": 0, "stdout": "v1"})
        old = s.session_run(["git", "rev-parse", "HEAD"]); s.bn_tx("review-hold", "first", ["update", s.first, "--status", "ready_for_review"], ["show", s.first], {"status": "ready_for_review"})
        s.pause_session(); s.bnrun("update", s.first, "--status", "in_progress", "--note", "please revise", actor="human"); s.resume_session()
        (s.repo / "feedback.txt").write_text("v2\n"); s.git_tx("commit-feedback", "first", ["commit", "-am", "v2"], ["log", "-1", "--pretty=%s"], {"returncode": 0, "stdout": "v2"}); new = s.session_run(["git", "rev-parse", "HEAD"]); assert old != new
        remote_ref = "refs/heads/bn-plan/feedback"; s.git_tx("push-feature", "first", ["push", "origin", "HEAD:" + remote_ref], ["ls-remote", "origin", remote_ref], {"returncode": 0, "stdout": new + "\t" + remote_ref})
        s.bn_tx("review-hold", "first", ["update", s.first, "--status", "ready_for_review"], ["show", s.first], {"status": "ready_for_review"})
        s.pause_session()
        approval = f"bn-plan-loop:v1 approve plan={s.plan} node=first head={new}"
        s.bnrun("update", s.first, "--status", "ready_for_validation", "--note", approval, actor="human")
        s.resume_session()
        detail = s.issue(s.first); logs = detail["log"]; assert logs[-2]["actor"] == logs[-1]["actor"] == "human" and approval in logs[-1]["event"]
        approval_commit = s.session_run(["git", "-C", str(s.hub), "rev-parse", "HEAD"])
        patch = s.session_run(["git", "-C", str(s.hub), "show", "--format=", approval_commit, "--", detail["path"]])
        assert "status ready_for_review → ready_for_validation" in patch and approval in patch
        s.bn_tx("merge-hold", "first", ["update", s.first, "--status", "ready_for_merge"], ["show", s.first], {"status": "ready_for_merge"}); assert s.issue(s.first)["status"] == "ready_for_merge"
        s.assert_safe_trace(); return {"scenario": "feedback-and-holds", "plan": s.plan, "approved_head": new}
    finally: s.cleanup()


def run(name: str, bn: Path) -> dict[str, str]:
    if name == "normal-two-node": return normal(bn)
    if name == "materialization-resume": return materialization_resume(bn)
    if name == "feedback-and-holds": return feedback_and_holds(bn)
    if name == "false-terminal": return false_terminal(bn)
    raise ValueError(name)


if __name__ == "__main__":
    p = argparse.ArgumentParser(); p.add_argument("--bn", required=True); p.add_argument("--scenario", required=True, choices=["normal-two-node", "materialization-resume", "feedback-and-holds", "false-terminal"])
    args = p.parse_args(); print(json.dumps(run(args.scenario, Path(args.bn)), sort_keys=True))
