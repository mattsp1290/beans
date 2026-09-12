---
name: bn-plan-loop
description: Execute one published, ready Beans plan as linked Beans issues on serial reviewable branches, with resumable human review, validation, merge holds, and Git ancestry verification. Use only inside an active goal as /goal $bn-plan-loop <plan-id>.
---

# Beans Plan Loop

Execute an already-published Beans plan. Beans is the workflow record; Git is the delivery record. Never infer delivery from issue status alone.

## Invocation

Accept exactly one nonempty plan ID after `$bn-plan-loop`. Reject paths, option-like IDs, and extra arguments. This workflow requires an active goal because it spans human waits. Outside one, perform no mutation and return exactly:

```text
/goal $bn-plan-loop <plan-id>
```

Run one executor per plan across machines. The local lease fences only checkouts sharing one Git common directory; `bn` has no cross-machine compare-and-swap claim.

## Start and preflight

1. Read [references/lifecycle.md](references/lifecycle.md) completely. Resolve the invocation repository, applicable instructions, one `bn` executable, project, plan, default branch, remotes, repository checks, and effective workflow. Run `bn prime` and verify the required command help before mutation.
2. Retrieve the published bundle into a new temporary child. Use `scripts/bn_plan_loop.py validate-contract` to validate the application context, execution map, source digests, graph coverage, dependency direction, and semantic digest. Stop on ambiguity; direct the plan author to revise with `$bn-implementation-plan`.
3. Feed the resolved gate evidence to `scripts/bn_plan_loop.py validate-preflight`. Require lifecycle `ready`, a clean checkout, an unclaimed checkout incarnation, a merge policy that preserves the reviewed head, all three `ready_for_*` hold states, and no binding outside the selected project/repository authority.
4. Start one `lease-session` and bind its owner/token to the checkout claim before the active phase. Keep that OS-locked session alive across every edit, Git command, review, and Beans mutation; route commands and fenced Beans transactions through its JSONL protocol. Release it only before a human wait while retaining the checkout lifetime claim.

## Materialize and dispatch

Read [references/materialization.md](references/materialization.md). Reconcile durable markers before creating anything. Link exactly one issue per executable node without `--force`, translate executable `precedes` edges into correctly directed blockers, verify cycles and plan status, then claim only the first runnable package in handoff order. Never dispatch unrelated globally-ready work.

## Execute one issue

Read [references/review-and-holds.md](references/review-and-holds.md). Create or recover the issue's fresh feature branch from a fetched immutable base OID. Note the bounded contract, implement only mapped paths, validate, stage explicit paths, commit normally, push the feature branch, and prove the remote head.

Run two independent reviews plus the installed maintainability review against the exact base/head pair. Apply required fixes through the installed fix workflow, revalidate, commit and push changes, rerun invalidated reviews, then publish evidence and enter `ready_for_review`.

At each human hold, snapshot with `scripts/monitor_bn.py`, release the executor lease, and yield to its read-only fingerprint monitor. Human approval must be the exact atomic `bn update` action described in the review reference. After approval, validate the approved remote head and enter `ready_for_merge`. Never merge or close for the human.

## Resume and finish

On every continuation, start a newly fenced persistent lease session and reconstruct phase from fresh Beans JSON, Git refs, issue notes, pending intents, and semantic plan digest. Follow the reconciliation table in the lifecycle reference. Do not start another issue until the previous terminal issue's exact reviewed head is an ancestor of refreshed base.

After every executable issue is terminal and delivered, run the plan integration gate. Retrieve a fresh bundle, change only lifecycle `ready` to `complete`, validate, publish, and verify `pushed: true`, hub `ahead: 0`, execution `done`, lifecycle `complete`, and no mismatch. Only then complete the active goal.

At every handoff report the plan, issue, branch, exact head/base, checks, current hold, and exact human action. At completion report issue coverage, containing base SHA, lifecycle, and hub sync state.

## Authority boundary

Allowed: mapped edits, reversible local checks, feature-branch commits and normal pushes, internal review/fixes, documented plan/issue mutations, and read-only monitoring.

Forbidden without separate explicit authority: merges, auto-merge, force-push, default-branch pushes, hook bypass, deployment, release, non-disposable migration, destructive cleanup, direct hub edits, unrelated issue work, or automatic parallel issue execution.
