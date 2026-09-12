#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import shutil
import subprocess
import sys
sys.dont_write_bytecode = True
import tempfile
import time
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "scripts"
sys.path.insert(0, str(SCRIPTS)); sys.path.insert(0, str(Path(__file__).parent))
import bn_plan_loop as helper
from assert_trace import assert_trace
from scenario_runner import run as run_scenario


class Tests(unittest.TestCase):
    def test_python_sources_compile(self):
        for path in sorted((ROOT / "scripts").glob("*.py")) + sorted(Path(__file__).parent.glob("*.py")):
            compile(path.read_text(), str(path), "exec")

    def make_bundle(self, root: Path) -> tuple[Path, dict]:
        bundle = root / "bundle"; sections = bundle / "sections"; sections.mkdir(parents=True)
        source = b"# Package\n\nDo work.  \n"
        normalized = helper.normalized_source(source)
        app = {"version": 1, "active_users": False, "backward_compatibility_required": False, "feature_flags": "not-applicable", "confirmed_at": "2026-09-12T01:35:16Z"}
        app["confirmation_digest"] = helper.sha256(helper.canonical(app))
        (sections / "00-overview.md").write_text("# Overview\n\n```implementation-plan\n" + json.dumps(app) + "\n```\n")
        (sections / "01-work.md").write_bytes(source)
        map_value = {"version": 1, "packages": [{"id": "work", "node_id": "work", "source": "sections/01-work.md", "source_digest": helper.sha256(normalized), "prerequisites": [], "paths": ["pkg/"], "validation": ["go test ./..."], "acceptance": ["tests pass"], "exclusions": ["no release"]}], "references": [{"node_id": "context", "reason": "context only"}]}
        graph = {"nodes": [{"id": "context"}, {"id": "work"}], "edges": []}
        (sections / "02-execution-handoff.md").write_text("# Handoff\n\n```bn-execution-map\n" + json.dumps(map_value) + "\n```\n")
        (bundle / "plan.md").write_text("---\nsections:\n  - sections/00-overview.md\n  - sections/01-work.md\n  - sections/02-execution-handoff.md\n---\n\n```bn-change-graph\n" + json.dumps(graph) + "\n```\n")
        return bundle, graph

    def test_contract_and_semantic_drift(self):
        with tempfile.TemporaryDirectory() as td:
            bundle, graph = self.make_bundle(Path(td)); first = helper.contract(bundle, graph)
            self.assertEqual(len(first["semantic_digest"]), 64)
            # Lifecycle/revision/ref are not inputs because graph refs and manifest are excluded.
            graph["nodes"][1]["ref"] = "fixture-1"
            self.assertEqual(first["semantic_digest"], helper.contract(bundle, graph)["semantic_digest"])
            graph["nodes"][1]["label"] = "Changed meaning"
            self.assertNotEqual(first["semantic_digest"], helper.contract(bundle, graph)["semantic_digest"])
            path = bundle / "sections/01-work.md"; path.write_text(path.read_text() + "changed\n")
            with self.assertRaises(helper.ContractError): helper.contract(bundle, graph)

    def test_contract_rejects_unknown_and_cycle(self):
        with tempfile.TemporaryDirectory() as td:
            bundle, graph = self.make_bundle(Path(td)); handoff = bundle / "sections/02-execution-handoff.md"
            text = handoff.read_text().replace('"references":', '"surprise": 1, "references":')
            handoff.write_text(text)
            with self.assertRaises(helper.ContractError): helper.contract(bundle, graph)
        with tempfile.TemporaryDirectory() as td:
            bundle, graph = self.make_bundle(Path(td)); handoff = bundle / "sections/02-execution-handoff.md"
            handoff.write_text("# Handoff\n\nmissing map\n")
            with self.assertRaises(helper.ContractError): helper.contract(bundle, graph)

    def test_application_context_boolean_enum_matrix(self):
        flags = ("appropriate", "not-appropriate", "decide-per-pr", "not-applicable")
        for active in (False, True):
            for compatible in (False, True):
                for choice in flags:
                    value = {"version": 1, "active_users": active, "backward_compatibility_required": compatible, "feature_flags": choice, "confirmed_at": "2026-09-12T01:35:16Z"}
                    value["confirmation_digest"] = helper.sha256(helper.canonical(value))
                    valid = (not active and not compatible) == (choice == "not-applicable")
                    if valid: helper.validate_application(value)
                    else:
                        with self.assertRaises(helper.ContractError): helper.validate_application(value)

    def test_approval_log_boundaries(self):
        marker = "bn-plan-loop:v1 approve plan=p node=n head=" + "a" * 40
        context = {"actor": "human", "repo": "repo", "sha": "aaaaaaa", "branch": "feature"}
        valid = {"log": [{**context, "event": "status ready_for_review → ready_for_validation"}, {**context, "event": "note — " + marker}]}
        self.assertTrue(helper.verify_approval_logs(valid, "p", "n", "a" * 40, "executor", "repo", "feature")["valid"])
        variants = [
            {"log": [valid["log"][1]]},
            {"log": [valid["log"][0], valid["log"][1], valid["log"][1]]},
            {"log": [{**valid["log"][0], "actor": "executor"}, {**valid["log"][1], "actor": "executor"}]},
            {"log": [valid["log"][0], {**valid["log"][1], "event": "note — " + marker.replace("a" * 40, "b" * 40)}]},
            {"log": [valid["log"][0], {**valid["log"][1], "sha": "different"}]},
        ]
        for issue in variants:
            with self.assertRaises(helper.ContractError): helper.verify_approval_logs(issue, "p", "n", "a" * 40, "executor", "repo", "feature")
        for repo, branch in (("wrong", "feature"), ("repo", "wrong")):
            with self.assertRaises(helper.ContractError): helper.verify_approval_logs(valid, "p", "n", "a" * 40, "executor", repo, branch)

    def test_preflight_failures_mutate_nothing(self):
        base = {"active_goal": True, "plan_id": "fixture-plan-ab12", "lifecycle_status": "ready", "dirty": [], "merge_preserves_head": True, "project": "fixture", "nodes": [{"node_id": "work", "executable": True, "binding": "unlinked", "issue_project": ""}, {"node_id": "context", "executable": False, "binding": "reference", "issue_project": ""}]}
        self.assertEqual(helper.validate_preflight(base)["plan_id"], base["plan_id"])
        invalid = []
        for key, value in (("active_goal", False), ("plan_id", "../plan"), ("lifecycle_status", "blocked"), ("dirty", ["M file"]), ("merge_preserves_head", False)):
            invalid.append({**base, key: value})
        invalid.append({**base, "nodes": [{"node_id": "work", "executable": True, "binding": "issue", "issue_project": "other"}]})
        invalid.append({**base, "nodes": [{"node_id": "context", "executable": False, "binding": "issue", "issue_project": "fixture"}]})
        with tempfile.TemporaryDirectory() as td:
            trace = Path(td) / "trace"; trace.write_text("")
            for value in invalid:
                with self.assertRaises(helper.ContractError): helper.validate_preflight(value)
            self.assertEqual(trace.read_text(), "")

    def test_started_and_unstarted_drift_policy(self):
        self.assertEqual(helper.classify_package_drift("same", "same", "in_progress", True), "unchanged")
        self.assertEqual(helper.classify_package_drift("old", "new", "unstarted", False), "reconcile_description")
        self.assertEqual(helper.classify_package_drift("old", "new", "unstarted", True), "human_disposition")
        for phase in ("in_progress", "ready_for_review", "ready_for_validation", "ready_for_merge"):
            self.assertEqual(helper.classify_package_drift("old", "new", phase, False), "human_disposition")

    def test_workflow_precedence_and_holds(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); hub = root / "hub.toml"; project = root / "project.toml"; explicit = root / "explicit.yaml"
            hub.write_text('[workflow]\ndefault="in_progress"\n'); project.write_text('[workflow]\nterminal=["done"]\n')
            wf = helper.load_workflow(None, project, hub); self.assertEqual(wf["default"], "in_progress"); self.assertEqual(wf["terminal"], ["done"])
            explicit.write_text("workflow:\n  default: open\n  terminal:\n    - done\n"); self.assertEqual(helper.load_workflow(explicit, project, hub)["terminal"], ["done"])
            yml = root / "valid.yml"; yml.write_text("workflow:\n  transitions:\n    open:\n      - in_progress\n")
            self.assertEqual(helper.load_workflow(yml, None, None)["transitions"], {"open": ["in_progress"]})
            oracle = SCRIPTS / "workflow_oracle.go"
            result = subprocess.run(["go", "run", str(oracle), "", str(project), str(hub)], cwd=ROOT.parents[2], capture_output=True, text=True, check=True)
            go_wf = json.loads(result.stdout)
            self.assertEqual(wf, go_wf)
            for name, raw in {
                "flow.yaml": "---\nworkflow: {default: open}\n",
                "comments.yml": "workflow:\n  default: open # accepted comment\n  transitions: {open: [in_progress]}\n",
                "partial.toml": '[workflow]\nactive=["open", "in_progress"]\n',
            }.items():
                path = root / name; path.write_text(raw)
                python_wf = helper.load_workflow(path, None, None)
                got = subprocess.run(["go", "run", str(oracle), str(path), "", ""], cwd=ROOT.parents[2], capture_output=True, text=True, check=True)
                oracle_wf = json.loads(got.stdout)
                self.assertEqual(python_wf, oracle_wf)
            bad = root / "bad.toml"; bad.write_text('[workflow]\nactive=["ready_for_review"]\n')
            with self.assertRaises(helper.ContractError): helper.load_workflow(bad, None, None)

    def test_state_cas(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); state = root / "state.json"
            self.assertEqual(helper.state_cas(state, 0, {"phase": "one"})["generation"], 1)
            with self.assertRaises(helper.ContractError): helper.state_cas(state, 0, {})

    def test_checkout_claim_and_intent(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); git_dir = root / "git"; git_dir.mkdir()
            self.assertEqual(helper.checkout_claim(git_dir, "plan-a", "inc-1", "owner", 1)["plan_id"], "plan-a")
            self.assertEqual(helper.checkout_claim(git_dir, "plan-a", "inc-1", "owner", 1)["incarnation"], "inc-1")
            self.assertEqual(helper.checkout_claim(git_dir, "plan-a", "inc-1", "resumed", 2)["fencing_token"], 2)
            with self.assertRaises(helper.ContractError): helper.checkout_claim(git_dir, "plan-b", "inc-2", "other", 2)
            self.assertTrue(helper.checkout_release(git_dir, "plan-a", "inc-1", "resumed", 2)["released"])
            self.assertEqual(helper.checkout_claim(git_dir, "plan-b", "inc-2", "other", 2)["plan_id"], "plan-b")
            state = root / "state.json"; value = helper.state_cas(state, 0, {"pending_intent": {"kind": "create"}})
            self.assertEqual(value["pending_intent"]["kind"], "create")

    def test_fenced_mutation_rejects_stale_owner(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); fake = root / "fake"; counter = root / "counter"; spec = root / "spec.json"; run_dir = root / "run"
            fake.write_text("#!/usr/bin/env python3\nimport json,pathlib,sys\np=pathlib.Path(sys.argv[2])\nif sys.argv[1]=='mutate': p.write_text(str(int(p.read_text() if p.exists() else '0')+1))\nelse: print(json.dumps({'status':'linked'}))\n")
            fake.chmod(0o755)
            spec.write_text(json.dumps({"intent": {"kind": "link", "plan_id": "p", "node_id": "n"}, "mutation": ["mutate", str(counter)], "verify": ["verify", str(counter)], "expect": {"status": "linked"}, "recovery": {"command": "plan_status", "format": "json", "argv": ["plan", "status", "p"], "expect": {"status": "linked"}}}))
            self.assertEqual(helper.fenced_bn(run_dir, "one", 0, str(fake), spec), 0)
            self.assertEqual(counter.read_text(), "1")
            with self.assertRaises(helper.ContractError): helper.fenced_bn(run_dir, "stale", 0, str(fake), spec)
            self.assertEqual(counter.read_text(), "1")

    def test_create_crash_before_stdout_reconciles_by_marker(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); fake = root / "fake"; database = root / "db.json"; run_dir = root / "run"; spec = root / "create.json"; recovery = root / "recover.json"
            marker = "<!-- bn-plan-loop:v1 plan=p node=n -->"
            fake.write_text("#!/usr/bin/env python3\nimport json,os,pathlib,signal,sys\np=pathlib.Path(os.environ['CRASH_DB'])\nif sys.argv[1]=='create':\n p.write_text(json.dumps({'id':'i','description':sys.argv[2]})); os.kill(os.getppid(), signal.SIGKILL)\nelif sys.argv[1]=='list': print(json.dumps([{'id':'i'}]))\nelif sys.argv[1]=='show' and sys.argv[2]=='i': print(p.read_text())\nelse: raise SystemExit(1)\n"); fake.chmod(0o755)
            intent = {"kind": "create", "plan_id": "p", "node_id": "n", "marker": marker}
            authority = ["--hub", str(root), "--project", "fixture", "--json"]
            recovery_contract = {"command": "issue_marker_search", "format": "json", "argv": ["list", "--label", "plan-p", "--archived", "--limit", "0", *authority], "show_argv_suffix": authority, "marker": marker, "expect": {"matches": 1}}
            invalid = {"intent": intent, "mutation": ["create", marker], "verify": ["show", "{mutation_stdout}"], "expect": {"description": marker}, "recovery": {**recovery_contract, "argv": ["list", "--label", "plan-p", "--archived", "--json"]}}
            with self.assertRaises(helper.ContractError):
                helper.execute_bn_locked(root / "invalid-state.json", {"generation": 0}, str(fake), invalid)
            for index, unsafe_expect in enumerate(({"matches": 0}, {"matches": 2}, {"issue_id": "i"})):
                unsafe = {**invalid, "recovery": {**recovery_contract, "expect": unsafe_expect}}
                with self.assertRaises(helper.ContractError):
                    helper.execute_bn_locked(root / f"unsafe-{index}.json", {"generation": 0}, str(fake), unsafe)
            ordinary_create = {**invalid, "recovery": {"command": "show_issue", "format": "json", "argv": ["show", "i"], "expect": {"id": "i"}}}
            with self.assertRaises(helper.ContractError):
                helper.execute_bn_locked(root / "ordinary-create.json", {"generation": 0}, str(fake), ordinary_create)
            spec.write_text(json.dumps({"intent": intent, "mutation": ["create", marker], "verify": ["show", "{mutation_stdout}"], "expect": {"description": marker}, "recovery": recovery_contract}))
            crashed_env = os.environ.copy(); crashed_env["CRASH_DB"] = str(database)
            crashed = subprocess.run([sys.executable, str(SCRIPTS / "bn_plan_loop.py"), "fenced-bn", "--run-dir", str(run_dir), "--owner", "creator", "--expected-token", "0", "--bn", str(fake), "--spec", str(spec)], env=crashed_env)
            self.assertNotEqual(crashed.returncode, 0); self.assertTrue(database.exists())
            state = json.loads((run_dir / "state.json").read_text()); self.assertEqual(state["pending_intent"]["marker"], marker)
            wrong = root / "wrong.json"; wrong.write_text(json.dumps({"intent": {**intent, "marker": "different"}}))
            with mock.patch.dict(os.environ, {"CRASH_DB": str(database)}):
                with self.assertRaises(helper.ContractError): helper.reconcile_bn(run_dir, "wrong", 1, str(fake), wrong)
            self.assertIsNotNone(json.loads((run_dir / "state.json").read_text())["pending_intent"])
            recovery.write_text(json.dumps({"intent": intent}))
            with mock.patch.dict(os.environ, {"CRASH_DB": str(database)}):
                with self.assertRaises(helper.ContractError): helper.reconcile_bn(run_dir, "wrong-executable", 1, "/usr/bin/true", recovery)
                self.assertEqual(helper.reconcile_bn(run_dir, "recovery", 1, str(fake), recovery), 0)
            self.assertIsNone(json.loads((run_dir / "state.json").read_text())["pending_intent"])

    def test_git_recovery_is_bound_to_git(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); repo = root / "repo"; run_dir = root / "run"; run_dir.mkdir()
            subprocess.run(["git", "init", "-b", "main", str(repo)], check=True, capture_output=True)
            state_path = run_dir / "state.json"; state = {"generation": 0, "fencing_token": 1}; state_path.write_text(json.dumps(state))
            verify = ["-C", str(repo), "branch", "--show-current"]
            spec = {"intent": {"kind": "branch-create", "plan_id": "p", "node_id": "n"}, "mutation": ["-C", str(repo), "checkout", "-b", "feature"], "verify": verify,
                    "expect": {"returncode": 0, "stdout": "forced-failure"}, "recovery": {"command": "git_verify", "format": "text", "argv": verify, "expect": {"returncode": 0, "stdout": "feature"}}}
            self.assertNotEqual(helper.execute_git_locked(state_path, state, spec), 0)
            identity = root / "identity.json"; identity.write_text(json.dumps({"intent": spec["intent"]}))
            with self.assertRaises(helper.ContractError): helper.reconcile_bn(run_dir, "wrong", 1, "/usr/bin/true", identity)
            self.assertEqual(helper.reconcile_bn(run_dir, "right", 1, "git", identity), 0)

    def test_prepared_intent_crash_clears_without_replay(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); fake = root / "fake"; touched = root / "mutated"; state_path = root / "run" / "state.json"
            fake.write_text("#!/usr/bin/env python3\nimport pathlib,sys\nif sys.argv[1]=='mutate': pathlib.Path(sys.argv[2]).write_text('mutated')\n")
            fake.chmod(0o755)
            intent = {"kind": "update", "plan_id": "p", "node_id": "n"}
            spec = {"intent": intent, "mutation": ["mutate", str(touched)], "verify": ["show", str(touched)], "expect": {"status": "done"},
                    "recovery": {"command": "show_issue", "format": "json", "argv": ["show", str(touched)], "expect": {"status": "done"}}}
            pid = os.fork()
            if pid == 0:
                helper.execute_bn_locked(state_path, {"generation": 0, "fencing_token": 1}, str(fake), spec, lambda: os.kill(os.getpid(), signal.SIGKILL))
                os._exit(99)
            _, status = os.waitpid(pid, 0); self.assertTrue(os.WIFSIGNALED(status)); self.assertFalse(touched.exists())
            persisted = json.loads(state_path.read_text())["pending_intent"]
            self.assertEqual(persisted["phase"], "prepared"); self.assertEqual(persisted["mutation_argv"], spec["mutation"])
            identity = root / "identity.json"; identity.write_text(json.dumps({"intent": intent}))
            self.assertEqual(helper.reconcile_bn(root / "run", "recovery", 1, str(fake), identity), 0)
            recovered = json.loads(state_path.read_text()); self.assertIsNone(recovered["pending_intent"]); self.assertTrue(recovered["recovered_unstarted"])

    def test_gated_child_parent_crash_never_executes_bn_or_git(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); fake = root / "fake"; touched = root / "mutated"; identity = root / "identity.json"
            fake.write_text("#!/usr/bin/env python3\nimport pathlib,sys\npathlib.Path(sys.argv[2]).write_text('mutated')\n"); fake.chmod(0o755)
            bn_intent = {"kind": "update", "plan_id": "p", "node_id": "bn"}; identity.write_text(json.dumps({"intent": bn_intent}))
            bn_spec = {"intent": bn_intent, "mutation": ["mutate", str(touched)], "verify": ["show", str(touched)], "expect": {"status": "done"},
                       "recovery": {"command": "show_issue", "format": "json", "argv": ["show", str(touched)], "expect": {"status": "done"}}}
            bn_state = root / "bn-run" / "state.json"
            pid = os.fork()
            if pid == 0:
                helper.execute_bn_locked(bn_state, {"generation": 0, "fencing_token": 1}, str(fake), bn_spec, after_started=lambda: os.kill(os.getpid(), signal.SIGKILL))
                os._exit(99)
            os.waitpid(pid, 0); pending = json.loads(bn_state.read_text())["pending_intent"]
            for _ in range(100):
                if not helper.process_exists(pending["child_pid"]): break
                time.sleep(0.01)
            self.assertFalse(touched.exists()); self.assertEqual(helper.reconcile_bn(root / "bn-run", "recover", 1, str(fake), identity), 0)

            repo = root / "repo"; subprocess.run(["git", "init", "-b", "main", str(repo)], check=True, capture_output=True)
            git_intent = {"kind": "branch-create", "plan_id": "p", "node_id": "git"}; identity.write_text(json.dumps({"intent": git_intent}))
            verify = ["-C", str(repo), "branch", "--show-current"]
            git_spec = {"intent": git_intent, "mutation": ["-C", str(repo), "checkout", "-b", "forbidden"], "verify": verify, "expect": {"returncode": 0, "stdout": "forbidden"},
                        "recovery": {"command": "git_verify", "format": "text", "argv": verify, "expect": {"returncode": 0, "stdout": "forbidden"}}}
            git_state = root / "git-run" / "state.json"; pid = os.fork()
            if pid == 0:
                helper.execute_git_locked(git_state, {"generation": 0, "fencing_token": 1}, git_spec, after_started=lambda: os.kill(os.getpid(), signal.SIGKILL))
                os._exit(99)
            os.waitpid(pid, 0); pending = json.loads(git_state.read_text())["pending_intent"]
            for _ in range(100):
                if not helper.process_exists(pending["child_pid"]): break
                time.sleep(0.01)
            branch = subprocess.run(["git", "-C", str(repo), "branch", "--show-current"], check=True, capture_output=True, text=True).stdout.strip()
            self.assertEqual(branch, "main"); self.assertEqual(helper.reconcile_bn(root / "git-run", "recover", 1, "git", identity), 0)

    def test_persistent_session_excludes_competing_iteration(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); run_dir = root / "run"; touched = root / "winner"; script = SCRIPTS / "bn_plan_loop.py"
            winner = subprocess.Popen([sys.executable, str(script), "lease-session", "--run-dir", str(run_dir), "--owner", "winner"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            ready = json.loads(winner.stdout.readline()); self.assertTrue(ready["ready"])
            loser = subprocess.run([sys.executable, str(script), "lease-session", "--run-dir", str(run_dir), "--owner", "loser"], input=json.dumps({"op": "run", "argv": [sys.executable, "-c", f"open({str(root / 'loser')!r},'w').write('bad')"]}) + "\n", capture_output=True, text=True)
            self.assertEqual(loser.returncode, 2); self.assertFalse((root / "loser").exists())
            winner.stdin.write(json.dumps({"op": "run", "argv": [sys.executable, "-c", f"open({str(touched)!r},'w').write('ok')"]}) + "\n"); winner.stdin.flush()
            self.assertEqual(json.loads(winner.stdout.readline())["returncode"], 0)
            repo = root / "repo"; subprocess.run(["git", "init", "-b", "main", str(repo)], check=True, capture_output=True)
            git_spec = {"intent": {"kind": "branch-create", "plan_id": "p", "node_id": "n"}, "mutation": ["-C", str(repo), "checkout", "-b", "feature"], "verify": ["-C", str(repo), "branch", "--show-current"], "expect": {"returncode": 0, "stdout": "feature"}, "recovery": {"command": "git_verify", "format": "text", "argv": ["-C", str(repo), "branch", "--show-current"], "expect": {"returncode": 0, "stdout": "feature"}}}
            winner.stdin.write(json.dumps({"op": "git", "spec": git_spec}) + "\n"); winner.stdin.flush(); self.assertEqual(json.loads(winner.stdout.readline())["returncode"], 0)
            with self.assertRaises(helper.ContractError): helper.execute_git_locked(root / "bad-state.json", {"generation": 0}, {**git_spec, "expect": {}})
            winner.stdin.write(json.dumps({"op": "release"}) + "\n"); winner.stdin.flush(); self.assertTrue(json.loads(winner.stdout.readline())["released"]); winner.communicate()
            self.assertEqual(touched.read_text(), "ok")
            fake = root / "fake"; counter = root / "counter"
            fake.write_text("#!/usr/bin/env python3\nimport json,pathlib,sys\np=pathlib.Path(sys.argv[2])\nif sys.argv[1]=='mutate': p.write_text(str(int(p.read_text() if p.exists() else '0')+1))\nelse: print(json.dumps({'status':'different'}))\n"); fake.chmod(0o755)
            failed = subprocess.Popen([sys.executable, str(script), "lease-session", "--run-dir", str(run_dir), "--owner", "failed"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            failed.stdout.readline()
            spec = {"intent": {"kind": "update", "plan_id": "p", "node_id": "n"}, "mutation": ["mutate", str(counter)], "verify": ["verify", str(counter)], "expect": {"status": "linked"}, "recovery": {"command": "show_issue", "format": "json", "argv": ["show", str(counter)], "expect": {"status": "linked"}}}
            failed.stdin.write(json.dumps({"op": "bn", "bn": str(fake), "spec": spec}) + "\n"); failed.stdin.flush(); self.assertNotEqual(json.loads(failed.stdout.readline())["returncode"], 0)
            failed.stdin.write(json.dumps({"op": "bn", "bn": str(fake), "spec": spec}) + "\n"); failed.stdin.flush(); self.assertEqual(json.loads(failed.stdout.readline())["returncode"], 4)
            self.assertEqual(counter.read_text(), "1")
            failed.stdin.write(json.dumps({"op": "abort"}) + "\n"); failed.stdin.flush(); self.assertTrue(json.loads(failed.stdout.readline())["aborted"]); failed.communicate()

    def test_argv_payload_is_inert(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); out = root / "out"; script = SCRIPTS / "bn_plan_loop.py"
            malicious = "spaces `touch nope` $(touch nope2) --flag\nmarkdown"
            session = subprocess.Popen([sys.executable, str(script), "lease-session", "--run-dir", str(root / "run"), "--owner", "argv"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            session.stdout.readline(); request = {"op": "run", "argv": [sys.executable, "-c", "import pathlib,sys; pathlib.Path(sys.argv[1]).write_text(sys.argv[2])", str(out), malicious]}
            session.stdin.write(json.dumps(request) + "\n"); session.stdin.flush(); self.assertEqual(json.loads(session.stdout.readline())["returncode"], 0)
            session.stdin.write(json.dumps({"op": "release"}) + "\n"); session.stdin.flush(); session.stdout.readline(); session.communicate()
            self.assertEqual(out.read_text(), malicious); self.assertFalse((Path.cwd() / "nope").exists())

    def test_monitor_startup_change_and_no_sync(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); state = root / "state.json"; trace = root / "trace"; lock = root / "monitor.lock"
            state.write_text(json.dumps({"responses": {"show i": {"id": "i", "status": "ready_for_review"}, "plan status p": {"plan_id": "p"}, "status": {"ahead": 0}}}))
            env = os.environ.copy(); env.update({"BN_PLAN_LOOP_FIXTURE_ROOT": str(root), "BN_PLAN_LOOP_FAKE_STATE": str(state), "BN_PLAN_LOOP_TRACE": str(trace)})
            fake = Path(__file__).parent / "fake-bn.py"; monitor = SCRIPTS / "monitor_bn.py"
            snap = subprocess.run([sys.executable, str(monitor), "snapshot", "--bn", str(fake), "--plan", "p", "--issue", "i", "--lock", str(lock)], env=env, capture_output=True, text=True, check=True)
            expected = json.loads(snap.stdout)["fingerprint"]
            data = json.loads(state.read_text()); data["responses"]["show i"] = {"id": "i", "status": "ready_for_validation"}; state.write_text(json.dumps(data))
            changed = subprocess.run([sys.executable, str(monitor), "monitor", "--bn", str(fake), "--plan", "p", "--issue", "i", "--lock", str(lock), "--expected", expected, "--interval", "0", "--max-ticks", "1"], env=env, capture_output=True, text=True)
            self.assertEqual(changed.returncode, 0); self.assertTrue(json.loads(changed.stdout)["changed"]); assert_trace(trace, True)

    def test_monitor_detects_remote_only_change(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); state = root / "state.json"; trace = root / "trace"; lock = root / "monitor.lock"; bare = root / "remote.git"; work = root / "work"
            subprocess.run(["git", "init", "--bare", str(bare)], check=True, capture_output=True)
            subprocess.run(["git", "init", "-b", "main", str(work)], check=True, capture_output=True)
            for key, value in (("user.name", "Eval"), ("user.email", "eval@example.invalid")): subprocess.run(["git", "-C", str(work), "config", key, value], check=True)
            (work / "x").write_text("one\n"); subprocess.run(["git", "-C", str(work), "add", "x"], check=True); subprocess.run(["git", "-C", str(work), "commit", "-m", "one"], check=True, capture_output=True)
            subprocess.run(["git", "-C", str(work), "push", str(bare), "HEAD:main"], check=True, capture_output=True)
            state.write_text(json.dumps({"responses": {"show i": {"id": "i", "status": "ready_for_merge"}, "plan status p": {"plan_id": "p"}, "status": {"ahead": 0}}}))
            env = os.environ.copy(); env.update({"BN_PLAN_LOOP_FIXTURE_ROOT": str(root), "BN_PLAN_LOOP_FAKE_STATE": str(state), "BN_PLAN_LOOP_TRACE": str(trace)})
            fake = Path(__file__).parent / "fake-bn.py"; monitor = SCRIPTS / "monitor_bn.py"
            common = ["--bn", str(fake), "--plan", "p", "--issue", "i", "--lock", str(lock), "--remote", str(bare), "--remote-branch", "main"]
            snap = subprocess.run([sys.executable, str(monitor), "snapshot", *common], env=env, capture_output=True, text=True, check=True)
            expected = json.loads(snap.stdout)["fingerprint"]
            (work / "x").write_text("two\n"); subprocess.run(["git", "-C", str(work), "commit", "-am", "two"], check=True, capture_output=True); subprocess.run(["git", "-C", str(work), "push", str(bare), "HEAD:main"], check=True, capture_output=True)
            changed = subprocess.run([sys.executable, str(monitor), "monitor", *common, "--expected", expected, "--interval", "0", "--max-ticks", "1"], env=env, capture_output=True, text=True)
            self.assertEqual(changed.returncode, 0); self.assertTrue(json.loads(changed.stdout)["changed"])

    def test_monitor_overlap_and_transient_retries(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); state = root / "state.json"; trace = root / "trace"; lock = root / "monitor.lock"
            stable = {"show i": {"id": "i"}, "plan status p": {"plan_id": "p"}, "status": {"ahead": 0}}
            state.write_text(json.dumps({"responses": stable})); env = os.environ.copy(); env.update({"BN_PLAN_LOOP_FIXTURE_ROOT": str(root), "BN_PLAN_LOOP_FAKE_STATE": str(state), "BN_PLAN_LOOP_TRACE": str(trace)})
            fake = Path(__file__).parent / "fake-bn.py"; monitor = SCRIPTS / "monitor_bn.py"; common = ["--bn", str(fake), "--plan", "p", "--issue", "i", "--lock", str(lock)]
            snap = subprocess.run([sys.executable, str(monitor), "snapshot", *common], env=env, capture_output=True, text=True, check=True); expected = json.loads(snap.stdout)["fingerprint"]
            first = subprocess.Popen([sys.executable, str(monitor), "monitor", *common, "--expected", expected, "--interval", ".2", "--max-ticks", "10"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, text=True)
            time.sleep(.1)
            overlap = subprocess.run([sys.executable, str(monitor), "monitor", *common, "--expected", expected, "--interval", "0", "--max-ticks", "1"], env=env, capture_output=True, text=True)
            self.assertEqual(overlap.returncode, 3); first.terminate(); first.wait()
            state.write_text(json.dumps({"responses": {**stable, "status": [{"error": "temporary", "code": 1}, {"error": "temporary", "code": 1}, {"ahead": 0}]}}))
            retried = subprocess.run([sys.executable, str(monitor), "monitor", *common, "--expected", "different", "--interval", "0", "--max-ticks", "1"], env=env, capture_output=True, text=True)
            self.assertEqual(retried.returncode, 0); self.assertTrue(json.loads(retried.stdout)["changed"])

    def test_structure_and_cases(self):
        skill = (ROOT / "SKILL.md").read_text(); self.assertIn("name: bn-plan-loop", skill)
        for rel in re.findall(r"\((references/[^)]+)\)", skill): self.assertTrue((ROOT / rel).is_file(), rel)
        cases = {p.name for p in (Path(__file__).parent / "cases").iterdir() if p.is_dir()}
        self.assertEqual(cases, {"normal-two-node", "materialization-resume", "feedback-and-holds", "false-terminal", "concurrent-start", "contract-validation"})
        for path in ROOT.rglob("*.md"):
            text = path.read_text(); self.assertNotIn("git push --force", text); self.assertNotIn("git merge ", text)

    def test_real_bn_scenarios(self):
        bn = ROOT.parents[2] / "bin" / "bn"
        if not bn.is_file(): self.skipTest("build ./bin/bn before real CLI scenarios")
        with tempfile.TemporaryDirectory() as td:
            external_home = Path(td) / "external-beans"; external_home.mkdir(); sentinel = external_home / "config.toml"; sentinel.write_text("sentinel = true\n")
            with mock.patch.dict(os.environ, {"BEANS_HOME": str(external_home)}):
                from fixture_repo import create
                fixture = create(bn)
            self.assertEqual(sentinel.read_text(), "sentinel = true\n")
            fixture_root = Path(fixture["root"]); self.assertTrue(fixture_root.name.startswith("bn-plan-loop-eval-")); shutil.rmtree(fixture_root)
        for scenario in ("normal-two-node", "materialization-resume", "feedback-and-holds", "false-terminal"):
            self.assertEqual(run_scenario(scenario, bn)["scenario"], scenario)


if __name__ == "__main__":
    unittest.main(verbosity=2)
