# Multi-Repo Workspace Routing Plan

## Problem

beans can dispatch issues from a tracker and run an agent in a
workspace, but repository selection is still implicit. Today an operator can
create `bn` issues from `~/git/birbparty/clckr`, `~/git/boxy`, or
`~/git/shady`, but that does not make those local repos visible to the
orchestrator host. The issue description must tell the agent where to clone or
what to do, and the runtime has no first-class notion of "this issue belongs to
repo X".

That is not enough for an always-on multi-repo agent system. We need repo
onboarding to be a durable operation, issue routing to be structured data, and
workspace preparation to be deterministic.

## Goal

Make beans support arbitrary repository onboarding and route each
issue to the right prepared repo workspace without requiring bespoke workflow
files, prompt conventions, or manual container mounts per repo.

The operator experience should become:

```bash
bn repo add boxy --path ~/git/boxy --remote git@github.com:punk1290/boxy.git
bn repo add shady --path ~/git/shady --remote git@github.com:punk1290/shady.git
bn repo add clckr --path ~/git/birbparty/clckr --remote git@github.com:punk1290/clckr.git

cd ~/git/boxy
bn create "Fix flaky timer test" -d "..."       # repo inferred from .bn repo marker

cd ~/git/shady
bn create "Add import validation" -d "..."      # repo inferred from .bn repo marker
```

The orchestrator should then:

1. Poll unblocked issues from the shared Postgres tracker.
2. Read the issue's repo target from tracker metadata.
3. Resolve the repo target through the repo registry.
4. Prepare an isolated per-run checkout under `/workspace`.
5. Run the agent with cwd at that checkout.
6. Persist run history with repo identity, revision, and workspace path.

## Non-Goals

- Do not turn Symphony into a general CI system.
- Do not require the orchestrator to see the user's Mac filesystem.
- Do not make repo routing depend on labels or natural-language parsing.
- Do not support cross-repo atomic commits in the first implementation.
- Do not solve PR creation across all forges in this plan. It can be a later
  post-run action.

## Core Decision

Use the Postgres-backed `bn` tracker as the system of record for repo onboarding
and issue-to-repo routing. Keep `WORKFLOW.md` for runtime defaults and policy,
but move mutable repo inventory out of `WORKFLOW.md`.

Rationale:

- `bn` already works from any local repo through `BN_DSN`.
- The orchestrator already supports `tracker.kind: postgres`.
- Postgres gives us transactionality for repo records, issue metadata, locks,
  and migrations.
- Repo onboarding needs frequent mutation; editing and redeploying
  `WORKFLOW.md` for each repo is the wrong operational shape.

## Plan Files

- [01-architecture.md](01-architecture.md): target architecture and runtime flow.
- [02-data-model.md](02-data-model.md): database and domain model changes.
- [03-bn-cli.md](03-bn-cli.md): repo onboarding and issue authoring UX.
- [04-workspace-routing.md](04-workspace-routing.md): workspace manager and checkout lifecycle.
- [05-host-deployment.md](05-host-deployment.md): infra/Spark/local-machine deployment shape.
- [06-security.md](06-security.md): trust boundaries and credential handling.
- [07-rollout.md](07-rollout.md): implementation sequence.
- [08-acceptance-tests.md](08-acceptance-tests.md): test plan and done criteria.

