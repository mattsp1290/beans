# Architecture

## Current Shape

The runtime has these main boundaries:

- `cmd/bn`: Postgres-backed tracker authoring CLI.
- `internal/tracker/postgres`: `bn` store plus tracker adapter.
- `internal/runtime`: wires config, persistence, tracker, workspace, worker,
  dispatcher, and poller.
- `internal/workspace`: creates per-issue workspace directories and runs hooks.
- `internal/worker`: runs the agent in a workspace.

The missing boundary is a durable repository registry. The workspace manager
only knows a root directory and an issue ID; it does not know which repo an
issue targets or how to materialize it.

## Target Shape

Add a repo registry and a workspace router:

```text
bn repo add/update/list
        |
        v
Postgres repo registry
        |
        v
tracker adapter returns Issue + RepoTarget metadata
        |
        v
workspace router resolves repo target
        |
        v
workspace manager prepares isolated checkout
        |
        v
worker runs agent in prepared repo checkout
```

## New Components

### `internal/repository`

Owns repo registry domain logic:

- Resolve repo by slug, ID, marker, or issue metadata.
- Validate repo config.
- Redact credentials in logs and snapshots.
- Decide clone/ref strategy.

This should be separate from `internal/tracker/postgres` so future trackers can
reuse the same repo concepts.

### `internal/repository/postgres`

Stores repo registry rows in the same Postgres database as `bn`.

Initial tables live in the `bn_` namespace unless we deliberately introduce a
new `repo_` namespace. Prefer `bn_repos` because the registry is part of the
tracker authoring surface.

### `internal/workspace/router`

Converts `(issue, repo target)` into a concrete workspace:

- Base path:
  `/workspace/repos/<repo_slug>/issues/<issue_id>/attempts/<attempt>/checkout`.
- Checkout source: mirror cache, git clone, or local host mirror.
- Branch/ref: issue requested ref, repo default branch, or generated work branch.
- Cwd: repo root or configured subdir.

This layer should produce a small immutable `PreparedWorkspace` value consumed
by the worker.

The current `workspace.Manager.Open(issue.Identifier)` API is not sufficient for
this because it creates one sanitized leaf below `workspace.root`. The workspace
router either needs a new manager API for structured repo/issue/attempt paths or
must own path creation itself while reusing the existing containment and
sanitization rules. The old issue-keyed cleanup path must be replaced for
repo-routed issues.

### Runtime Wiring

`runtime.Build` should construct:

- The tracker adapter.
- The repo registry store.
- The workspace router.

`runtime.WirePoller` should pass repo-aware workspace preparation into the
dispatch path. Run-attempt audit insertion must happen before checkout
preparation so repo checkout failures produce durable run rows, run events, and
OTel spans. The dispatch path should then fail before agent construction when
repo resolution or checkout preparation fails.

## Issue Lifecycle

1. User runs `bn create` from an onboarded repo.
2. `bn` writes an issue row with `repo_id` and optional `repo_ref`.
3. Orchestrator polls ready issues.
4. Tracker adapter includes repo routing metadata in `core.Issue`.
5. Dispatcher reserves the issue.
6. Runtime creates the run-attempt row.
7. Workspace router prepares checkout.
8. Worker runs agent with cwd at the prepared workspace cwd.
9. Agent closes issue via `tracker_write`.
10. Runtime finalizes the run attempt with repo identity and checked-out
    revision.

## Failure Semantics

Repo resolution and checkout preparation failures should be retryable by
default because they are often transient:

- Git remote unavailable.
- SSH deploy key temporarily unavailable.
- Repo mirror lock busy.
- Requested branch missing due to recent push lag.

Permanent validation failures should be surfaced clearly:

- Issue has no repo target and no default repo can be inferred.
- Repo slug does not exist.
- Repo is disabled.
- Repo URL scheme is disallowed by policy.
- Repo host is not allowed by deployment policy.
- Repo `auth_ref` is missing or incompatible with the remote host.

## Compatibility

Existing single-repo deployments keep working:

- If no `repo_id` is present on an issue, the router keeps the existing
  repo-less workspace behavior. A default repo fallback requires a future
  explicit config-schema change because strict `WORKFLOW.md` validation rejects
  unknown `workspace.*` keys today.
- Beads tracker mode remains issue-description based until a beads metadata
  bridge exists.
- Current `workspace.root` remains valid as the parent of repo workspaces.
