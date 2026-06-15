# Deploy bean-counter to production (10.0.0.106)

Plan owner: deploy workstream • Target host: `infra-admin@10.0.0.106` •
Status: **revision 2 — reviewed by two independent Opus reviewers; all findings
folded in (see `07` reconciliation log)**

## Goal

Stand up the bean-counter Go API + Svelte UI on the infra host
(`10.0.0.106`) and give operators a repeatable, audited one-command deploy
from a developer workstation:

```bash
scripts/deploy-production.sh --ref main
```

bean-counter is a dashboard over the **beans** issue tracker. In production it
points at the **existing** Postgres that the `local-symphony` orchestrator
stack already runs on `10.0.0.106` — the same database the `bn` CLI and the
orchestrator write. bean-counter becomes a live read/write web view of that
tracker.

## Confirmed decisions (operator)

These three decisions were made by the operator and drive the whole design:

1. **Database topology — share the production beans DB.** The API connects to
   the existing `local-symphony` Postgres (database the orchestrator uses),
   with `BN_PROJECT_PREFIX=local-symphony`. It does **not** run its own
   Postgres. → see `01-architecture-and-topology.md`.
2. **Network exposure — expose on a LAN port.** The UI is published on
   `0.0.0.0:<UI_PORT>` (default `8088`), reachable at `http://10.0.0.106:8088`.
   bean-counter has **no authentication**, so this is unauthenticated LAN
   access to a read/write tracker view. Accepted; documented as a risk.
3. **Write posture — full read/write.** All mutating endpoints
   (`POST/PATCH/DELETE /api/v1/issues`, `/deps`, `/close`) are live. The
   mandatory pre-deploy `pg_dump` plus generated `rollback.md` are the safety
   net. → see `04-safety-and-rollback.md`.

## What we are reusing

The `local-symphony` repo already contains a battle-tested deploy harness for
this exact host. We mirror it rather than invent a new one:

- `~/git/local-symphony/deploy/update-production.sh` — the reference script
  (SHA pinning to `origin/main`, single `flock`'d remote SSH session,
  mandatory `pg_dump` before mutation, secret-safe payloads, audit records,
  generated `rollback.md`). bean-counter's `scripts/deploy-production.sh`
  follows the same contract and safety model.
- `~/git/local-symphony/.agents/plans/production-deploy-script/` — the doc
  structure this plan mirrors.

## Architecture (grounded against source)

| Component | Source of truth | Notes |
|-----------|-----------------|-------|
| Go API | `cmd/bean-counter/main.go`, `internal/{config,server,store,handlers}` | Fiber v3. Wraps `github.com/mattsp1290/beans/store`. Config from `BN_*` env. |
| Svelte UI | `frontend/` (Vite + Svelte 5, nginx) | `frontend/Dockerfile` builds static assets; nginx serves them and proxies `/api/` to `API_UPSTREAM`. |
| API image | `Dockerfile` | Multi-stage, CGO-free static binary, runs as non-root. |
| UI image | `frontend/Dockerfile` | nginx:1.27-alpine, templated `default.conf`. |
| Existing prod stack ref | `docker-compose.stack.yml` | api+ui+db for local full-stack; **production drops the `db` service** (we use the shared Postgres). |

API endpoints (from source): liveness `GET /api/v1/healthz`
(`internal/server/app.go:39`), readiness `GET /api/v1/readyz`
(`internal/handlers/health/health.go:20`, verifies store + project),
data routes under `/api/v1/{issues,deps,ready,graph}`. UI liveness is nginx
`GET /healthz` (`frontend/nginx.conf`).

## The single biggest risk: beans schema/version skew

bean-counter pins `github.com/mattsp1290/beans v0.1.1` (`go.mod`). The
`beans/store` package **owns schema migrations** — `internal/store/adapter.go`
explicitly says bean-counter "must not add its own beans-table migrations,"
and `main.go` calls `adapter.EnsureProject(ctx)` at startup. Pointing
bean-counter at the orchestrator's live database means:

- `beansstore.New(...)` calls `schema.Migrate` (goose, **forward-only**,
  tracked in `bn_schema_versions`, advisory-locked) on first connect.
- The risk is **one-directional**: bean-counter *newer* than prod would apply
  migrations to the production schema and could break the orchestrator;
  bean-counter *at or below* prod is a guaranteed no-op.

This is treated as a **blocking preflight gate**: compare bean-counter's embedded
max migration to `select max(version_id) from bn_schema_versions` on the shared
DB and abort if bean-counter is newer — plus a mandatory backup. Not a footnote.
Details in `04-safety-and-rollback.md` and the task sequence.

## Document map

- `01-architecture-and-topology.md` — host layout, networking, DB connectivity, secrets, ports.
- `02-script-contract.md` — `scripts/deploy-production.sh` CLI, modes, and the production compose file.
- `03-implementation-phases.md` — local gates → remote deploy phases.
- `04-safety-and-rollback.md` — backups, shared-DB blast radius, version skew, rollback.
- `05-validation.md` — smoke tests and the script's own unit tests.
- `06-task-sequence.md` — ordered, dependency-aware task list (beads-ready).
- `07-open-questions-and-review.md` — risks, open items, reviewer reconciliation log.
