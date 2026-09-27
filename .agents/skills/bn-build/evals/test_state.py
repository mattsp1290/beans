"""Independent black-box checks against real helper subprocesses, Git and bn."""
from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
import copy
import fcntl
import json
from pathlib import Path
import select
import subprocess
import sys
import tempfile
import time
import unittest

from assert_trace import assert_trace
from fixture_repo import Fixture, HELPER, build_bn


class StateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="bn-mvp-evaluations-")
        cls.suite_root = Path(cls.temporary.name).resolve()
        cls.binary = build_bn(cls.suite_root)

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def setUp(self):
        self.f = Fixture(self.suite_root / self._testMethodName, self.binary)
        self.addCleanup(self.verify_fixture)

    def verify_fixture(self):
        self.f.verify_isolation()
        assert_trace(self.f.trace, self.suite_root)

    @contextmanager
    def keeper(self, actor="builder", token="fixture-token-long"):
        f = self.f
        argv = [sys.executable, str(HELPER), "hold-lock", "--repo", str(f.sources[actor]),
                "--contract", str(f.contract_path), "--token", token]
        process = subprocess.Popen(argv, cwd=f.sources[actor], env=f.envs[actor],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            readable, _, _ = select.select([process.stdout], [], [], 10)
            self.assertTrue(readable, "keeper did not report readiness")
            line = process.stdout.readline()
            self.assertTrue(line, process.stderr.read() if process.poll() is not None else "keeper silent")
            ready = json.loads(line)
            self.assertNotIn("error", ready, ready)
            self.assertIsNone(process.poll(), ready)
            yield process
        finally:
            process.terminate()
            try:
                process.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.communicate()

    def state(self, op, *args, actor="builder", succeeds=True):
        process, data = self.f.state(op, *args, actor=actor, check=False)
        if succeeds:
            self.assertEqual(process.returncode, 0, data)
        else:
            self.assertNotEqual(process.returncode, 0, data)
        return data

    def reserve(self, actor="builder", succeeds=True, *args):
        return self.state("reserve-target", "--token", "fixture-token-long", *args, actor=actor, succeeds=succeeds)

    def envelope(self, key, kind, data):
        digest = self.state("validate-contract")["scope_digest"]
        return {"key": key, "type": kind, "scope_digest": digest, "data": data}

    def record(self, envelope, succeeds=True):
        path = self.f.write_json("record-input.json", envelope)
        return self.state("record", "--token", "fixture-token-long", "--record", str(path), succeeds=succeeds)

    def reconcile(self, envelopes, **kwargs):
        path = self.f.write_json("notes-input.json", envelopes)
        return self.state("reconcile", "--token", "fixture-token-long", "--notes", str(path), **kwargs)

    def publish(self, *envelopes):
        for envelope in envelopes:
            self.f.bn("note", self.f.root_id, "bn-mvp:v1 " + json.dumps(envelope, separators=(",", ":")))

    def live_reconcile(self):
        return self.state("reconcile", "--token", "fixture-token-long", "--hub",
                          self.f.envs["builder"]["BEANS_HUB"], "--bn", str(self.f.binary))

    def target(self):
        self.f.git("checkout", "-b", "mvp/fixture")
        self.f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")

    def change(self, filename="first.txt"):
        (self.f.sources["builder"] / filename).write_text("tested fixture content\n")
        self.f.git("add", filename)
        self.f.git("commit", "-m", "fixture: " + filename)
        return self.f.git("rev-parse", "HEAD").stdout.strip()

    def test_contract_validation_and_semantic_digest(self):
        f = self.f
        expected = self.state("validate-contract")["scope_digest"]
        note_path = f.root / "description.md"
        note_path.write_text("Notes outside contract\n```bn-mvp-contract\n" + json.dumps(f.contract) + "\n```\n")
        original = f.contract_path
        f.contract_path = note_path
        self.assertEqual(self.state("validate-contract")["scope_digest"], expected)
        note_path.write_text(note_path.read_text() + "Unrelated note\n")
        self.assertEqual(self.state("validate-contract")["scope_digest"], expected)
        note_path.write_text(note_path.read_text() + "```bn-mvp-contract\n{}\n```\n")
        self.state("validate-contract", succeeds=False)
        f.contract_path = original
        variants = []
        for field, value in (("version", True), ("scope_revision", 0), ("max_in_flight", True),
                             ("acceptance", []), ("checks", []), ("target_ref", "refs/heads/main"),
                             ("repository", "https://secret@example.invalid/repo"), ("remote", "-x"),
                             ("milestone_id", "--bad"), ("base_sha", "abc")):
            invalid = copy.deepcopy(f.contract); invalid[field] = value; variants.append(invalid)
        invalid = copy.deepcopy(f.contract); invalid["slices"][0]["depends_on"] = [f.second]; variants.append(invalid)
        invalid = copy.deepcopy(f.contract); invalid["checks"][0]["cwd"] = "../escape"; variants.append(invalid)
        outside = f.root / "outside"; outside.mkdir()
        (f.sources["builder"] / "escape").symlink_to(outside)
        invalid = copy.deepcopy(f.contract); invalid["checks"][0]["cwd"] = "escape"; variants.append(invalid)
        for number, invalid in enumerate(variants):
            with self.subTest(invalid=number):
                f.contract_path.write_text(json.dumps(invalid))
                self.state("validate-contract", succeeds=False)
        f.contract_path.write_text(json.dumps(f.contract))

    def test_two_clone_atomic_ownership_and_milestone_binding(self):
        f = self.f
        before = {actor: f.git("rev-parse", "HEAD", actor=actor, cwd=Path(f.envs[actor]["BEANS_HUB"])).stdout for actor in f.envs}
        with self.keeper(), self.keeper("auditor"):
            with ThreadPoolExecutor(max_workers=2) as pool:
                results = list(pool.map(lambda actor: f.state("reserve-target", "--token", "fixture-token-long", actor=actor, check=False), f.envs))
            self.assertEqual(sorted(result.returncode for result, data in results), [0, 1], results)
            winner = list(f.envs)[next(i for i, item in enumerate(results) if item[0].returncode == 0)]
            loser = "auditor" if winner == "builder" else "builder"
            self.reserve(winner)
            # Changing target and key cannot bypass the root milestone binding.
            changed = copy.deepcopy(f.contract); changed["target_ref"] = "refs/heads/mvp/alternate"
            (f.sources[loser] / ".git/bn-mvp" / f.contract["key"] / "owner.json").unlink()
            f.contract_path.write_text(json.dumps(changed))
            self.reserve(loser, succeeds=False)
            f.contract_path.write_text(json.dumps(f.contract))
        for actor in f.envs:
            self.assertEqual(f.git("rev-parse", "HEAD", actor=actor, cwd=Path(f.envs[actor]["BEANS_HUB"])).stdout, before[actor])
            self.assertEqual(f.git("status", "--porcelain", actor=actor).stdout, "")

    def test_keeper_liveness_and_lost_cache_fail_closed(self):
        f = self.f
        with self.keeper():
            self.reserve()
            self.state("check-owner", "--token", "wrong", succeeds=False)
            duplicate = f.run([sys.executable, str(HELPER), "hold-lock", "--repo", str(f.sources["builder"]),
                               "--contract", str(f.contract_path), "--token", "other"], check=False)
            self.assertNotEqual(duplicate.returncode, 0)
            self.state("check-owner", "--token", "fixture-token-long")
        self.state("check-owner", "--token", "fixture-token-long", succeeds=False)
        cache = f.sources["builder"] / ".git/bn-mvp" / f.contract["key"] / "owner.json"
        cache.unlink()
        with self.keeper():
            self.reserve(succeeds=False)

    def test_forbidden_discovered_default_and_remote_failure(self):
        f = self.f
        # Remote default changes to a non-main branch; lying default_ref cannot
        # authorize using the actually advertised default as an MVP target.
        f.git("push", "origin", "HEAD:refs/heads/trunk")
        f.git("symbolic-ref", "HEAD", "refs/heads/trunk", cwd=f.code_remote)
        invalid = copy.deepcopy(f.contract); invalid["target_ref"] = "refs/heads/trunk"
        f.contract_path.write_text(json.dumps(invalid))
        with self.keeper():
            self.reserve(succeeds=False)
            f.contract_path.write_text(json.dumps(f.contract))
            f.git("symbolic-ref", "HEAD", "refs/heads/main", cwd=f.code_remote)
            hook = f.code_remote / "hooks/pre-receive"
            hook.write_text("#!/bin/sh\nexit 1\n"); hook.chmod(0o755)
            self.reserve(succeeds=False)
            refs = f.git("for-each-ref", "--format=%(refname)", cwd=f.code_remote).stdout
            self.assertNotIn("bn-mvp-owner", refs)
            hook.unlink()
            f.git("config", "receive.advertiseAtomic", "false", cwd=f.code_remote)
            self.reserve(succeeds=False)
            self.assertNotIn("bn-mvp-owner", f.git("for-each-ref", "--format=%(refname)", cwd=f.code_remote).stdout)
            f.git("config", "receive.advertiseAtomic", "true", cwd=f.code_remote)
            self.reserve()

    def test_existing_target_requires_explicit_selection(self):
        self.target()
        with self.keeper():
            self.reserve(succeeds=False)
            self.state("reserve-target", "--token", "fixture-token-long", "--allow-existing-target")

    def test_assignment_recovery_default_and_overridden_ready(self):
        f = self.f
        with self.keeper():
            self.reserve()
            self.target()
            partial = f.sources["builder"] / "partial.txt"; partial.write_text("preserve interrupted work\n")
            assignment = self.envelope("assign-first", "assignment", {
                "slice_id": f.first, "assignment_id": "worker-first", "branch": "refs/heads/work/first",
                "worktree": str(f.sources["builder"]), "status": "active"})
            recorded = self.record(assignment)
            self.assertEqual(recorded["note"], self.record(assignment)["note"])
            changed = copy.deepcopy(assignment); changed["data"]["status"] = "stopped"
            self.record(changed, succeeds=False)
            duplicate_worker = copy.deepcopy(assignment); duplicate_worker["key"] = "duplicate-worker"
            duplicate_worker["data"]["assignment_id"] = "worker-duplicate"
            self.record(duplicate_worker, succeeds=False)
            # Publish through real bn; cache alone is not publication proof.
            f.bn("note", f.root_id, "bn-mvp:v1 " + json.dumps(assignment, separators=(",", ":")))
            f.bn("update", f.first, "--claim")
            self.assertNotIn(f.first, {row["id"] for row in f.bn("ready")})
            recovered = self.reconcile(f.bn("show", f.root_id))
            self.assertNotIn(f.first, recovered["dispatchable"])
            self.assertEqual(recovered["assignments"][0]["assignment_id"], "worker-first")
            workflow = f.root / "workflow.toml"
            workflow.write_text('[workflow]\nactive = ["open", "in_progress"]\n')
            f.envs["builder"]["BN_CONFIG"] = str(workflow)
            self.assertIn(f.first, {row["id"] for row in f.bn("ready")})
            recovered = self.reconcile(f.bn("show", f.root_id))
            self.assertNotIn(f.first, recovered["dispatchable"])
            self.assertNotIn(f.second, recovered["dispatchable"])
            self.assertEqual(partial.read_text(), "preserve interrupted work\n")
            del f.envs["builder"]["BN_CONFIG"]
            cache = f.sources["builder"] / ".git/bn-mvp" / f.contract["key"] / "records.json"
            cache.unlink()  # Simulate restart with only published assignment evidence.
            recovered = self.state("reconcile", "--token", "fixture-token-long", "--hub", f.envs["builder"]["BEANS_HUB"], "--bn", str(f.binary))
            self.assertNotIn(f.first, recovered["dispatchable"])
            self.assertEqual(recovered["assignments"][0]["assignment_id"], "worker-first")

    def test_review_checks_false_close_and_target_ancestry(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            head = self.change()
            # bn closed blockers become ready independently of target integration.
            f.bn("close", f.first, "--reason", "False completion for evaluation")
            self.assertIn(f.second, {row["id"] for row in f.bn("ready")})
            self.assertNotIn(f.second, self.reconcile([])["dispatchable"])
            review = self.envelope("review-first", "review", {"slice_id": f.first, "base_sha": f.base_sha,
                "head_sha": head, "reviewer": "independent-fixture-reviewer", "verdict": "pass", "evidence": ["fixture review artifact"]})
            self.record(review)
            failed = f.run(["false"], check=False)
            checks = [{"argv": ["false"], "cwd": ".", "exit_code": failed.returncode}]
            integration = self.envelope("integrate-first", "integration", {"slice_id": f.first,
                "base_sha": f.base_sha, "head_sha": head, "integrated_sha": head,
                "checks": checks, "reviews": [review["key"]]})
            self.record(integration, succeeds=False)
            passed = f.run(["true"])
            integration["data"]["checks"] = [{"argv": ["true"], "cwd": ".", "exit_code": passed.returncode}]
            self.record(integration, succeeds=False)  # Commit is absent from remote target.
            f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
            stale = copy.deepcopy(integration); stale["data"]["head_sha"] = f.base_sha
            self.record(stale, succeeds=False)
            self.record(integration)
            self.assertNotIn(f.first, self.reconcile([review])["integrated"])

            for note in (review, integration):
                f.bn("note", f.root_id, "bn-mvp:v1 " + json.dumps(note, separators=(",", ":")))
            reconciled = self.state("reconcile", "--token", "fixture-token-long", "--hub", f.envs["builder"]["BEANS_HUB"], "--bn", str(f.binary))
            self.assertIn(f.first, reconciled["integrated"])
            self.assertIn(f.second, reconciled["dispatchable"])
            fake = copy.deepcopy(integration); fake["key"] = "false-integration"
            fake["data"]["integrated_sha"] = "0" * 40
            self.assertTrue(self.reconcile([review, fake])["blocked"])

    def test_two_clone_audit_throttle_sync_and_failure(self):
        f = self.f
        self.assertEqual(f.bn("request", "list"), [])  # Establish fetch throttle.
        body = f.root / "audit.md"; body.write_text("Audit snapshot: missing first file\n")
        request = f.bn("request", "create", "Acceptance regression", "--body-file", str(body),
                       "--issue", f.root_id, actor="auditor")
        self.assertEqual(f.bn("request", "list"), [])  # Real stale read, no fake bn.
        with self.keeper():
            self.reserve()
            barrier_args = ["--token", "fixture-token-long", "--hub", f.envs["builder"]["BEANS_HUB"], "--bn", str(f.binary)]
            self.state("sync-barrier", *barrier_args)
            self.assertEqual([row["id"] for row in f.bn("request", "list")], [request["id"]])
            hub = Path(f.envs["builder"]["BEANS_HUB"])
            f.git("remote", "set-url", "origin", str(f.root / "missing.git"), cwd=hub)
            self.state("sync-barrier", *barrier_args, succeeds=False)
            f.git("remote", "set-url", "origin", str(f.hub_remote), cwd=hub)

    def test_merge_integration_requires_review_of_actual_target_parent(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            f.git("checkout", "-b", "work/first")
            worker_head = self.change("first.txt")
            old_review = self.envelope("review-old-base", "review", {
                "slice_id": f.first, "base_sha": f.base_sha, "head_sha": worker_head,
                "reviewer": "schema-reviewer", "verdict": "pass", "evidence": ["Fixture review of A/C"]})
            self.record(old_review)
            f.git("checkout", "mvp/fixture")
            advanced_target = self.change("target-advance.txt")
            f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
            f.git("merge", "--no-ff", "work/first", "-m", "fixture: merge worker onto advanced target")
            merged = f.git("rev-parse", "HEAD").stdout.strip()
            self.assertEqual(f.git("show", "-s", "--format=%P", merged).stdout.split(),
                             [advanced_target, worker_head])
            f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
            check = f.run(["true"])
            integration = self.envelope("merge-integration", "integration", {
                "slice_id": f.first, "base_sha": f.base_sha, "head_sha": worker_head,
                "integrated_sha": merged, "checks": [{"argv": ["true"], "cwd": ".", "exit_code": check.returncode}],
                "reviews": [old_review["key"]]})
            rejected = self.record(integration, succeeds=False)
            self.assertIn("reviewed target base", rejected["message"])
            relabeled = copy.deepcopy(integration)
            relabeled["key"] = "side-parent-as-fast-forward"
            relabeled["data"]["integrated_sha"] = worker_head
            # C reached the target only through M's second parent. Labeling it
            # a historical fast-forward cannot bypass review of B/C.
            self.assertEqual(self.record(relabeled, succeeds=False)["error"], "stale")
            new_review = copy.deepcopy(old_review)
            new_review["key"] = "review-current-base"
            new_review["data"]["base_sha"] = advanced_target
            new_review["data"]["evidence"] = ["Fixture refreshed review of B/C"]
            self.record(new_review)
            integration["data"]["base_sha"] = advanced_target
            integration["data"]["reviews"] = [new_review["key"]]
            self.record(integration)
            self.publish(new_review, integration)
            self.assertIn(f.first, self.live_reconcile()["integrated"])
            # B really was the earlier fast-forward target and remains in M's
            # first-parent history. Its historical evidence must stay valid.
            historical_review = self.envelope("historical-ff-review", "review", {
                "slice_id": f.second, "base_sha": f.base_sha, "head_sha": advanced_target,
                "reviewer": "schema-reviewer", "verdict": "pass", "evidence": ["Fixture historical A/B review"]})
            self.record(historical_review)
            historical = self.envelope("historical-ff-integration", "integration", {
                "slice_id": f.second, "base_sha": f.base_sha, "head_sha": advanced_target,
                "integrated_sha": advanced_target, "checks": [{"argv": ["true"], "cwd": ".", "exit_code": check.returncode}],
                "reviews": [historical_review["key"]]})
            self.record(historical)

    def test_demo_requires_all_integrations_published_and_reconciled(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            check_spec = f.contract["checks"][0]
            executed = f.run(check_spec["argv"])
            check = {**check_spec, "exit_code": executed.returncode}
            demo = self.envelope("demo-complete", "demo", {
                "head_sha": f.base_sha, "checks": [check], "journey": ["Fixture journey pending"],
                "runtime_identity": "git:" + f.base_sha})
            self.assertEqual(self.record(demo, succeeds=False)["error"], "stale")
            evidence = []
            base = f.base_sha
            for index, slice_id in enumerate((f.first, f.second), 1):
                head = self.change(f"slice-{index}.txt")
                f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
                review = self.envelope(f"review-{index}", "review", {
                    "slice_id": slice_id, "base_sha": base, "head_sha": head,
                    "reviewer": "schema-reviewer", "verdict": "pass", "evidence": ["Fixture review metadata"]})
                integration = self.envelope(f"integration-{index}", "integration", {
                    "slice_id": slice_id, "base_sha": base, "head_sha": head,
                    "integrated_sha": head, "checks": [check], "reviews": [review["key"]]})
                self.record(review); self.record(integration)
                evidence.append((review, integration))
                base = head
            journey = f.run([sys.executable, "-c", "from pathlib import Path; print(Path('slice-1.txt').read_text() + Path('slice-2.txt').read_text())"])
            demo["data"].update(head_sha=head, runtime_identity="git:" + head,
                                journey=journey.stdout.strip().splitlines())
            # Both integration records exist only locally.
            self.assertEqual(self.record(demo, succeeds=False)["error"], "stale")
            self.publish(*evidence[0]); self.live_reconcile()
            # One published slice cannot qualify the whole milestone.
            self.assertEqual(self.record(demo, succeeds=False)["error"], "stale")
            self.publish(*evidence[1])
            offline = self.reconcile([record for pair in evidence for record in pair])
            self.assertEqual(offline["freshness"], "unverified")
            # Supplied note JSON cannot establish publication.
            self.assertEqual(self.record(demo, succeeds=False)["error"], "stale")
            self.assertEqual(set(self.live_reconcile()["integrated"]), {f.first, f.second})
            self.record(demo)

    def test_final_pr_requires_authoritatively_published_manual_acceptance(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            accepted = self.envelope("accepted-current", "manual-acceptance", {
                "head_sha": f.base_sha, "evidence": "Fixture acceptance metadata; no actual human test claimed"})
            final = self.envelope("final-current", "final-pr", {
                "head_sha": f.base_sha, "url": "https://example.invalid/pull/fixture"})
            self.record(accepted)
            self.record(final, succeeds=False)
            self.publish(accepted)
            self.record(final, succeeds=False)  # Publication has not been verified by the coordinator.
            self.reconcile([accepted])
            self.record(final, succeeds=False)  # Offline note import is insufficient.
            self.live_reconcile()
            result = self.record(final)
            self.assertEqual(result["record"]["data"]["head_sha"], f.base_sha)
            self.assertFalse(result["published"])  # Formatting a PR record does not create a PR.

    def test_high_risk_requires_two_reviewers_and_later_is_not_selected(self):
        f = self.f
        f.contract["slices"][0]["risk"] = "high"
        f.contract_path.write_text(json.dumps(f.contract))
        f.bn("update", f.root_id, "--description", "```bn-mvp-contract\n" + json.dumps(f.contract) + "\n```")
        later = f.bn("create", "Optional polish", "--label", "mvp:later", "--parent", f.root_id)["id"]
        with self.keeper():
            self.reserve(); self.target()
            head = self.change(); f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
            review = self.envelope("review-one", "review", {"slice_id": f.first, "base_sha": f.base_sha,
                "head_sha": head, "reviewer": "reviewer-one", "verdict": "pass", "evidence": ["schema fixture evidence"]})
            self.record(review)
            result = f.run(["true"])
            integration = self.envelope("integration", "integration", {"slice_id": f.first, "base_sha": f.base_sha,
                "head_sha": head, "integrated_sha": head, "checks": [{"argv": ["true"], "cwd": ".", "exit_code": result.returncode}],
                "reviews": ["review-one"]})
            self.record(integration, succeeds=False)
            review_two = copy.deepcopy(review); review_two["key"] = "review-two"
            self.record(review_two)
            integration["data"]["reviews"].append("review-two")
            self.record(integration, succeeds=False)  # Two keys from one reviewer are insufficient.
            review_three = copy.deepcopy(review); review_three["key"] = "review-three"
            review_three["data"]["reviewer"] = "reviewer-two"
            self.record(review_three)
            integration["data"]["reviews"].append("review-three")
            self.record(integration)
            self.assertIn(later, {row["id"] for row in f.bn("ready")})
            current = self.state("reconcile", "--token", "fixture-token-long", "--hub", f.envs["builder"]["BEANS_HUB"], "--bn", str(f.binary))
            self.assertNotIn(later, current["dispatchable"])

    def test_duplicate_findings_preserve_evidence_without_creating_repairs(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            disposition = self.envelope("finding-session-read", "finding-disposition", {
                "fingerprint": "fixture:session-read:missing-content", "disposition": "unverified",
                "evidence": "Old snapshot still requires current-target reproduction"})
            first = self.record(disposition)
            self.assertFalse(first["idempotent"])
            self.assertTrue(self.record(disposition)["idempotent"])
            # Duplicate audit publication is allowed by bn: uniqueness belongs to
            # the coordinator's actual reproduction/repair workflow.
            for actor in f.envs:
                f.bn("request", "create", "Duplicate snapshot finding", "--description", json.dumps(disposition),
                     "--issue", f.root_id, actor=actor)
            f.bn("sync")
            self.assertEqual(len(f.bn("request", "list")), 2)
            self.assertEqual(len(f.bn("list")), 3)
            conflict = copy.deepcopy(disposition); conflict["data"]["disposition"] = "accepted"
            self.record(conflict, succeeds=False)

    def test_sync_barrier_blocks_dirty_hub_and_lock_contention(self):
        f = self.f
        with self.keeper():
            self.reserve()
            hub = Path(f.envs["builder"]["BEANS_HUB"])
            args = ["--token", "fixture-token-long", "--hub", str(hub), "--bn", str(f.binary)]
            unknown = hub / "unknown.txt"; unknown.write_text("preserve this unrecognized change\n")
            self.state("sync-barrier", *args, succeeds=False)
            self.assertEqual(unknown.read_text(), "preserve this unrecognized change\n")
            unknown.unlink()
            remote_before = f.git("rev-parse", "HEAD", cwd=f.hub_remote).stdout
            pending = f.bn("note", f.root_id, "Interrupted fixture note; intent fixture-note", "--no-sync")
            self.assertFalse(pending["pushed"])
            self.state("sync-barrier", *args, succeeds=False)
            self.assertEqual(f.git("rev-parse", "HEAD", cwd=f.hub_remote).stdout, remote_before)
            intents = f.write_json("allowed-intents.json", [{"sha": pending["commit"], "intent_key": "fixture-note", "milestone_id": f.root_id}])
            self.state("sync-barrier", *args, "--allow-pending", str(intents))
            lockpath = Path(f.envs["builder"]["BEANS_HOME"]) / "cache/hub.lock"
            with lockpath.open("w") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                # bn's documented hub lock timeout is 30 seconds. Observe the
                # actual failure, rather than pretending a skipped fetch synced.
                result = f.run([sys.executable, str(HELPER), "sync-barrier", "--repo", str(f.sources["builder"]),
                                "--contract", str(f.contract_path), *args], check=False, timeout=45)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("error", json.loads(result.stdout))

    def test_sync_barrier_requires_exclusive_nondefault_home_and_hub(self):
        f = self.f
        original = dict(f.envs["builder"])
        simulated_home = f.root / "simulated-user"
        defaults = simulated_home / ".beans"
        defaults.mkdir(parents=True)
        sentinel = defaults / "config.toml"; sentinel.write_text("must remain untouched\n")
        invoked = f.root / "unexpected-bn-invocation"
        tripwire = f.root / "bn-tripwire"
        tripwire.write_text(f"#!{sys.executable}\nfrom pathlib import Path\nPath({str(invoked)!r}).touch()\nraise SystemExit(99)\n")
        tripwire.chmod(0o755)
        # Override only Python's home resolver, never HOME or a real user's
        # configuration. The public CLI runs unchanged through runpy. If the
        # early guard regresses, the tripwire prevents any Beans config access.
        launch = "from pathlib import Path; import runpy, sys; home=Path(sys.argv.pop(1)); Path.home=classmethod(lambda cls: home); sys.argv=sys.argv[1:]; sys.path.insert(0, str(Path(sys.argv[0]).parent)); runpy.run_path(sys.argv[0], run_name='__main__')"
        with self.keeper():
            self.reserve()
            hub_before = f.git("rev-parse", "HEAD", cwd=f.hub_remote).stdout
            try:
                for key, value in (("BEANS_HOME", None), ("BEANS_HUB", None),
                                   ("BEANS_HOME", str(defaults)), ("BEANS_HUB", str(defaults / "hub"))):
                    with self.subTest(key=key, value=value):
                        f.envs["builder"] = dict(original)
                        if value is None:
                            f.envs["builder"].pop(key)
                        else:
                            f.envs["builder"][key] = value
                        result = f.run([sys.executable, "-c", launch, str(simulated_home), str(HELPER),
                            "sync-barrier", "--repo", str(f.sources["builder"]), "--contract", str(f.contract_path),
                            "--token", "fixture-token-long", "--hub", original["BEANS_HUB"], "--bn", str(tripwire)], check=False)
                        self.assertNotEqual(result.returncode, 0)
                        self.assertEqual(json.loads(result.stdout)["error"], "identity")
                        self.assertFalse(invoked.exists(), "guard called bn before rejecting unsafe defaults")
            finally:
                f.envs["builder"] = original
            self.assertEqual(sentinel.read_text(), "must remain untouched\n")
            self.assertEqual(f.git("rev-parse", "HEAD", cwd=f.hub_remote).stdout, hub_before)

    def test_manual_acceptance_is_exact_sha(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            accepted = self.envelope("accepted-initial", "manual-acceptance", {"head_sha": f.base_sha,
                "evidence": "Human fixture event accepted initial content"})
            self.record(accepted)
            head = self.change(); f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
            stale = copy.deepcopy(accepted); stale["key"] = "accepted-stale"
            self.record(stale, succeeds=False)
            final = self.envelope("final-pr", "final-pr", {"head_sha": head, "url": "https://example.invalid/pull/1"})
            self.record(final, succeeds=False)
            status = self.reconcile([accepted])
            self.assertIn(accepted["key"], status["stale_records"])

    def test_recovery_after_target_push_note_publication_and_close(self):
        f = self.f
        with self.keeper():
            self.reserve(); self.target()
            head = self.change()
            review = self.envelope("review-before-interruption", "review", {
                "slice_id": f.first, "base_sha": f.base_sha, "head_sha": head,
                "reviewer": "schema-reviewer", "verdict": "pass", "evidence": ["Fixture review metadata"]})
            self.record(review); self.publish(review)
            f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
        # The coordinator ended after target publication but before recording it.
        with self.keeper():
            self.reserve()
            self.assertEqual(f.remote_sha(f.contract["target_ref"]), head)
            self.assertNotIn(f.first, self.live_reconcile()["integrated"])
            checked = f.run(["true"])
            integration = self.envelope("recovered-integration", "integration", {
                "slice_id": f.first, "base_sha": f.base_sha, "head_sha": head,
                "integrated_sha": head, "checks": [{"argv": ["true"], "cwd": ".", "exit_code": checked.returncode}],
                "reviews": [review["key"]]})
            self.record(integration); self.publish(integration)
        # Lose all local evidence after the note push, retaining ownership proof.
        (f.sources["builder"] / ".git/bn-mvp" / f.contract["key"] / "records.json").unlink()
        with self.keeper():
            recovered = self.live_reconcile()
            self.assertIn(f.first, recovered["integrated"])
            self.assertIn(f.second, recovered["dispatchable"])
            f.bn("close", f.first, "--reason", "Integrated on fixture target " + head)
        # Recover a lost close acknowledgement by inspection; do not repeat it.
        hub_head = f.git("rev-parse", "HEAD", cwd=f.hub_remote).stdout
        with self.keeper():
            self.assertIn(f.first, self.live_reconcile()["integrated"])
            self.assertEqual(f.bn("show", f.first)["status"], "closed")
            events = [entry["event"] for entry in f.bn("show", f.root_id)["log"]]
            self.assertEqual(sum('"key":"recovered-integration"' in event for event in events), 1)
            self.assertEqual(f.remote_sha(f.contract["target_ref"]), head)
            self.assertEqual(f.git("rev-parse", "HEAD", cwd=f.hub_remote).stdout, hub_head)

    def test_independent_worktree_processes_overlap_and_integrate(self):
        f = self.f
        f.contract["slices"][1]["depends_on"] = []
        f.contract_path.write_text(json.dumps(f.contract))
        f.bn("dep", "remove", f.second, f.first)
        f.bn("update", f.root_id, "--description", "```bn-mvp-contract\n" + json.dumps(f.contract) + "\n```")
        worktrees = [f.root / f"worker-{index}" for index in (1, 2)]
        worker = """import json, subprocess, sys, time
from pathlib import Path
marker, gate, filename = map(Path, sys.argv[1:])
marker.write_text('ready')
while not gate.exists(): time.sleep(.01)
started = time.monotonic()
filename.write_text('independent fixture result')
assert filename.read_text() == 'independent fixture result'
time.sleep(.2)
subprocess.run(['git', 'add', str(filename)], check=True, capture_output=True)
subprocess.run(['git', 'commit', '-m', 'fixture: independent worker'], check=True, capture_output=True)
head = subprocess.run(['git', 'rev-parse', 'HEAD'], check=True, text=True, capture_output=True).stdout.strip()
print(json.dumps({'started': started, 'ended': time.monotonic(), 'head': head}))
"""
        with self.keeper():
            self.reserve(); self.target()
            self.assertEqual(set(self.live_reconcile()["dispatchable"]), {f.first, f.second})
            for index, worktree in enumerate(worktrees, 1):
                f.git("worktree", "add", "-b", f"work/parallel-{index}", str(worktree), f.base_sha)
            gate = f.root / "workers-start"
            with ThreadPoolExecutor(max_workers=2) as pool:
                futures = [pool.submit(f.run, [sys.executable, "-c", worker, str(f.root / f"ready-{index}"),
                           str(gate), f"result-{index}.txt"], cwd=worktree)
                           for index, worktree in enumerate(worktrees, 1)]
                deadline = time.monotonic() + 10
                while not all((f.root / f"ready-{index}").exists() for index in (1, 2)):
                    self.assertLess(time.monotonic(), deadline, "workers did not start")
                    time.sleep(.01)
                gate.write_text("start")
                results = [json.loads(future.result().stdout) for future in futures]
            self.assertLess(max(result["started"] for result in results), min(result["ended"] for result in results))
            for index in (1, 2):
                f.git("merge", "--no-ff", f"work/parallel-{index}", "-m", f"fixture: integrate worker {index}")
            checked = f.run([sys.executable, "-c", "from pathlib import Path; assert all(Path(f'result-{i}.txt').read_text() == 'independent fixture result' for i in (1,2))"])
            self.assertEqual(checked.returncode, 0)
            f.git("push", "origin", "HEAD:refs/heads/mvp/fixture")
            for result in results:
                f.git("merge-base", "--is-ancestor", result["head"], f.remote_sha(f.contract["target_ref"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
