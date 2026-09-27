---
name: bn-build
description: Build one Beans MVP milestone with subagents and reviewed integration into its non-default target branch. Use inside a goal with a milestone issue ID.
---

# Build a Beans MVP

Use `/goal $bn-build <milestone-issue-id>`. Finish with a verified runnable demo
on the milestone target, ready for manual testing. A later accepted-revision
invocation prepares the final PR. Outside an active goal, return the invocation
without mutation. Accept one issue ID, not a path, global queue, or old plan ID.

## Start or recover

Read [the contract](references/contract.md) and
[ownership/recovery](references/state-and-recovery.md). Resolve the consumer repo,
its instructions, one `bn` binary, project, and the root issue. Use the required
exclusive role-specific hub clone for every Beans mutation/barrier. Reuse recorded
user decisions. Preserve unrelated work. Validate the contract and synchronize
Beans before deciding what can run. Reserve the target and keep its local lock
alive throughout coordinator work; never treat `bn --claim` as a distributed lock.

Recover unresolved assignments before choosing new work. A claimed slice can be
absent from `bn ready`; absence does not mean completion. Reconcile live children,
their branches, and Git ancestry before replacing a stopped assignment. Never
spawn a duplicate because a tool observation timed out. Read linked audit
requests separately from issues and record their disposition against current code.

## Build a bounded milestone

The coordinator alone assigns slices, mutates their workflow records, and writes
the target branch. Intersect ready issues with the contract's must-have set,
subtract active assignments and integrated work, and verify dependency SHAs on
the target. Leave `mvp:later` and unrelated work alone.

Use up to two independent implementation subagents and one reviewer within live
harness limits. Give each implementing child its own branch/worktree, a concrete
outcome, base SHA, acceptance checks, interfaces, and exclusions. Avoid overlapping
writers. Serialize shared seams. Use fewer agents when the critical path or
machine resources favor it. Give narrow tasks scoped context, not the entire
history by default. Children return commit/check evidence; they do not write the
target, close slices, or expand the milestone.

Paths are advisory within the authorized outcome and repository. Necessary
integration edits do not require a new plan. Crossing repositories, destructive
data changes, new product scope, or deployment still requires existing authority
or a specific user decision. File useful optional work in Beans with a revisit
trigger rather than expanding the critical path.

Apply [scoped review](references/review.md), then
[integrate and demonstrate](references/integration.md). Each passing slice merges
into the selected target and immediately unlocks dependent work. Do not compose
the old plan loop, ship pipeline, or a review gauntlet automatically. Keep all
repository-required checks and reviews.

## Stop conditions

- **Built:** all must-have slices are on the remote target; required checks and
  the real acceptance journey passed on that exact revision; fresh audit intake
  has no unresolved must-fix finding. Publish the demo handoff and complete the
  build-to-demo goal. Manual testing and final PR remain explicit.
- **Accepted follow-up:** verify the user's acceptance of the current target SHA,
  recheck audit/CI state, create or reconcile the final PR, record its actual URL,
  and complete that follow-up goal. Never merge to the default branch or deploy.
- **Blocked:** preserve work and evidence, continue independent must-have work
  when possible, otherwise report the exact unmet condition and follow the live
  goal harness's blocked-state rules. No endless polling or fabricated pass.

At handoff report the milestone ID, target/head, demo command or URL, acceptance
proof, limitations, deferred issues, and one next action. Use the consumer's
handoff mechanism for durable continuation context. Do not leave a goal active
solely to wait for a manual test.
