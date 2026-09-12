---
name: bn-implementation-plan
description: Create a repository-grounded implementation plan, refine it through two independent reviews and one adversarial review, and publish it to the Beans hub using bn plan commands. Use for detailed technical planning stored in Beans, including revisions to existing hub plans. Do not use for immediate implementation or a short checklist.
---

# Beans Implementation Plan

Planning and publication to the Beans hub are the deliverables. Do not implement the planned change unless separately requested. This adapts the implementation-plan workflow to native Beans plan bundles; it does not require that other skill to be installed.

## Establish the target and CLI

1. Resolve the source repository with `git rev-parse --show-toplevel`. Read applicable `AGENTS.md` and contributor guidance. Inspect the worktree without disturbing unrelated changes.
2. Run `bn prime`, `bn plan --help`, and `bn status --json`. Determine the intended Beans project from the invocation repository and user context. Use `--project <project>` consistently when an override is needed; do not assume every invocation targets `beans` or that the hub is always at its default path.
3. Verify that `plan init`, `validate`, `get`, `put`, `list`, and `show` exist. If the installed CLI is too old, use an available current binary. When working in the Beans source checkout, a local build with `go build -o bin/bn ./cmd/bn` is an appropriate fallback; use that binary consistently afterward. Do not replace the globally installed CLI. If no suitable CLI is available, finish useful research and report the missing capability without claiming publication.
4. Inspect `bn plan list --json` for related plans. For an explicit revision, use the existing plan ID. Do not overwrite an unrelated plan or silently revise an immutable `complete` plan.
5. Create a staging parent using `mktemp -d`, then use a nonexistent child directory for the bundle. Both `init` and `get` refuse existing destinations. For a new plan, run `bn plan init "<title>" --output <staging-parent>/bundle --json`. For a revision, run `bn plan get <plan-id> --output <staging-parent>/bundle --json`. Record the returned ID and destination.

The hub is the canonical destination. Staging is an editable working copy, not a second permanent plan under `.agents/plans/`. Never edit or commit hub files directly; `bn plan put` owns the write pipeline, commit, and push. Keep the staging copy until publication is verified, and report its location if work is interrupted.

## Research and operating context

Inspect the relevant code, tests, build configuration, documentation, interfaces, and call paths before asserting facts. Infer the change type and affected areas from the request and repository. Distinguish observed facts, proposals, assumptions, and decisions. Label new paths and symbols as proposed, with an existing parent or insertion point. Use repository-relative source references and identify the source repository and observed revision so the plan is portable from the hub.

Do not copy credentials or other secret values into plan files or reviewer prompts. Use variable names and redacted examples.

Before detailed planning, obtain explicit answers to the following operating-context questions, reusing answers already supplied in the conversation:

- Does this application, service, library, or tool have active users or external consumers?
- Must the change preserve compatibility for APIs, stored data, configuration, or established workflows?
- If either answer is yes, are feature flags `appropriate`, `not-appropriate`, or `decide-per-pr`?

Ask missing questions together and continue independent research while waiting. Do not infer these answers from code. If both booleans are false, record flags as `not-applicable`. Preserve the user's answers through review. Record them with their confirmation time in the overview's Application context section. A missing answer blocks implementation readiness. With `decide-per-pr`, identify every behavior-changing package that needs a decision before implementation.

Resolve other questions from repository evidence where possible. Classify remaining questions as blocking or non-blocking, and identify an owner and exact unblock action for each blocker. Apply compatibility, migration, rollout, rollback, and feature-flag decisions to the affected work packages.

## Author the bundle

Read [references/bundle-format.md](references/bundle-format.md) before editing. Preserve the generated identity and revision fields. Write the manifest and ordered sections with concrete behavior, dependencies, verification, and acceptance criteria. Keep the manifest summary, change graph, and detailed execution order consistent.

Represent external dependencies and requests inside listed bundle sections so they travel with the hub plan. Record the owner repository, demonstrated and prospective consumers, requested contract, exclusions, acceptance criteria, affected packages, and exact unblock evidence. Inspect available owner contracts and releases before proposing shared work. A request is neither owner acceptance nor a usable dependency. A required contract without a verified usable revision blocks readiness. Creating repositories, contacting owners, or filing issues in other projects requires scope from the user; planning alone does not authorize those actions.

Use `bn` for execution task tracking. Graph nodes can remain unlinked during planning; do not invent issue IDs or automatically create an issue for every node. If issue creation or linking is part of the requested scope, use the CLI and inspect its current help. `bn plan link <plan-id> <node-id> <issue-id>` binds an existing issue after publication; it does not create issue dependencies. Graph edges do not replace `bn dep` blockers. Retrieve a fresh bundle after link mutations before further edits.

## Review and revise

Read [references/review-protocol.md](references/review-protocol.md). Validate the initial bundle using `bn plan validate <bundle> --json`, then run the two independent reviewer subagents and the fresh adversarial reviewer specified there. The calling agent owns all edits and disposition of findings.

After the last review, re-read the complete bundle. Check user requirements, source references, section links, graph relationships, dependency order, acceptance criteria, and operating-context decisions. Remove contradictions and obsolete assumptions. Structural validation alone does not establish implementation readiness.

## Publish and verify

1. Set `status: ready` only when required reviews completed and no blocking decision remains. Use `status: blocked` for unresolved blockers, naming each owner and unblock action in a `BLOCKER:` Risks list item. Use `draft` for unfinished planning. Planning completion is not implementation completion: never set `complete` merely because this skill finished.
2. Run `bn plan validate <bundle> --json` after final edits. Fix validation errors before publication.
3. Run `bn plan put <bundle> --json`. A request to create a hub plan includes this publication; do not add another approval step unless an applicable restriction requires it. Record the actual ID, hub-relative path, lifecycle status, commit, `pushed`, and `local_revision_refreshed` result. Do not infer push success solely from exit code zero.
4. On a stale-revision error, retrieve the published plan into a new staging child, merge the intended changes into that fresh copy, preserve its revision token and concurrent edits, and validate again. Never forge `updated` or use a force overwrite to bypass concurrency. Re-review substantive design changes. Retry publication once after reconciliation; if contention continues, preserve the copies and report the exact blocker.
5. If publication reports a pending push, inspect `bn status --json` and use `bn sync` to retry once. Inspect the published ID before repeating a failed mutation; a failed push may already have committed locally. If sync fails, report the local publication and pending remote state accurately.
6. Verify with `bn plan show <plan-id> --json` and `bn status --json`. Retrieve the result into another new directory with `bn plan get` and validate that bundle; compare the manifest and sections with the reviewed content, allowing CLI-owned revision changes. If `local_revision_refreshed` is false, use a fresh retrieval for future edits. Do not discard local differences automatically.

Deliver the actual plan ID, lifecycle status, hub-relative path, and verified publication state. Link the published manifest when its absolute location is known, and give `bn plan show <plan-id>` as the portable entry point. State the intended outcome, important decisions or blockers, review completion, and the first implementation action (or exact unblock action). Distinguish readiness from publication and state that implementation has not occurred. A bundle in this format is not automatically compatible with tools expecting a local `00-overview.md` plan directory, such as the original plan-pr-loop.
