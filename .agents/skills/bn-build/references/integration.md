# Integrate, demonstrate, and hand off

## Publish a slice to the target

1. Fetch the named source remote; check the live owner/lock and contract. Resolve
   the exact current target SHA. Confirm each required review names that base and
   the candidate head, with no unresolved blocking finding.
2. If target advanced, merge it into the slice branch normally, resolve conflicts,
   run affected checks, and obtain a final review attestation for the new pair.
   Do not rewrite published history or assume an earlier green result survives.
3. In a separate integration worktree, create the combined candidate preserving
   the reviewed head (merge commit or fast-forward; no automatic squash). Run
   combined checks. Keep source/runtime/check evidence tied to the actual tree.
4. Recheck owner and publish with a normal fast-forward target push. If rejected,
   fetch, reconcile and retry once after affected checks/review. Continued
   contention blocks publication. Never force-push to win a race.
5. Read the remote target back and prove both candidate and reviewed head are
   ancestors. Append integration evidence through `bn`, require synchronized
   publication, then close the slice with target and integrated SHA in its reason.
6. Start the next eligible slice without a human hold. Slice closure means
   integrated into this target, not released or merged to main.

Check command results are reusable only for the same tree, command,
toolchain/environment fingerprint and relevant dependency inputs. Run affected
checks after changes and the full repository gate at milestone end. Do not repeat
unchanged costly checks solely to restate progress. Failed required checks are
never papered over with a passing unrelated test.

If the source push succeeds but evidence/close fails, recover by reading Git and
Beans state before retrying. Do not merge twice. If a terminal issue's commit is
absent from target, stop its dependent slices and repair the record. Preserve
partial work from stopped children. Remove only agent-created clean temporary
worktrees whose commits are reachable; retain branches/evidence by default.

## Fresh audit intake

At entry/continuation, before the demo handoff, and before final PR creation,
perform the synchronization barrier in [state and recovery](state-and-recovery.md).
Read `bn request list/show --json` separately from `bn ready`; request records do
not enter the issue queue automatically. Resolve links to this root, inspect
unseen finding fingerprints, and record disposition against the current target.

Reproduce stale findings against current code. Accepted must-fix findings reopen
building and create one bounded repair task per fingerprint. Deferred findings
remain outside `slices`. Record the synchronized hub SHA and inspected request set
as the cutoff; a later audit can reopen work but cannot retrospectively be covered
by an earlier approval. A stopped builder is resumed explicitly using
`/goal $bn-build <root-id>`; publishing a request does not wake another goal.

## Actual product acceptance

After all must-have slices are integrated, start the consumer's supported app or
tool from the exact target revision and run its recorded acceptance journey.
For a UI, interact with the actual route and controls in a browser. Verify the
served build/process is the target revision; a screenshot or unit test alone does
not establish it. Use disposable data and the approved environment. Missing
credentials/hardware do not turn a required check into a pass.

Run the full repository gate and any required assembled review, then fresh audit
intake. Publish the target/head, runtime identity, command/URL, acceptance
checklist/results, limitations and deferred issue IDs. Record a `demo` event and
root phase `ready-for-manual-test`. Complete the build-to-demo goal; keep the root
issue open. Do not leave the goal polling for the human.

## Manual test and final PR

On the user's follow-up, identify the revision they tested. If the current remote
target differs, show the delta and obtain renewed acceptance for changed behavior.
Re-run invalidated evidence, check current CI, synchronize/triage audits, and
verify no must-fix findings remain. The contract's automation permission alone is
not manual acceptance.

Record the user's exact-revision acceptance. Resolve existing PRs for this source
branch first; create the final PR to the discovered default branch only after
acceptance. Use the repository's preflight and PR conventions. Record the actual
URL and head, then close the root with an explicit “accepted and PR opened; not
merged to main” reason. Complete the PR-preparation follow-up goal. The user
merges the final PR; these skills neither write the default branch nor deploy.

If manual testing or a later audit finds a required defect, reopen the milestone,
deduplicate/create its repair issue, and resume building. Mark an existing final
PR unready when its acceptance is invalidated. Keep context decisions; do not
repeat the operating-context questionnaire.
