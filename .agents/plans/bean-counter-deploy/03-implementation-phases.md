# 03 — Implementation phases

Two halves: **local gates** run on the operator's workstation; the **remote
deploy** runs entirely inside one `flock`'d SSH session on the infra host so
concurrent deploys cannot interleave (exact model of
`local-symphony/deploy/update-production.sh`).

## A. Local target resolution & preconditions

1. `resolve_target_sha` — `git fetch origin`; resolve `--ref` to `TARGET_SHA`
   under the pushed-ref policy (`main == origin/main`, or a SHA reachable from
   `origin/main`). Reject tags/branches.
2. `require_clean_local_ref`:
   - worktree clean (no tracked changes / untracked files);
   - `go.mod` has **no** `replace` for `github.com/mattsp1290/beans` (a local
     replace must not ship — grounded against `go.mod`);
   - `go list -m -json github.com/mattsp1290/beans` succeeds.

## B. Local gates (live mode)

Recorded into a `LOCAL_PREFLIGHT` summary shipped to the remote deploy record.
Reuse the repo's existing `make` targets / CI invocations so the gate spellings
do not drift from what the project actually runs.

1. `make test` (= `go test ./...`). Note this **already includes** the
   `test/e2e/sqlite_smoke_test.go` suite — it has no build tag, so it runs here
   (against SQLite tempdirs, safe).
2. integration gate (skippable with `--skip-integration`, recorded):
   `make test-integration` (= `go test -tags=integration ./...`), matching the
   matrix CI job in `.github/workflows/ci.yml`. **These tests use testcontainers
   (`postgres.Run` / mysql), so they need a working local Docker daemon — not a
   reachable Postgres.** State this operability requirement to operators.
3. static gates: `make vet`, `make lint`, `make fmt-check` (all exist).
4. frontend gates (in `frontend/`):
   - `npm ci`,
   - `npm run check` (svelte-check + tsc),
   - `npm test` (vitest — 4 test files exist, non-empty),
   - `npm run build` (proves the Vite build the UI image depends on).
5. local docker build gate (skippable with `--skip-local-build`, recorded):
   - `docker build -t <api-image> --label org.opencontainers.image.revision=<sha> .`
   - `docker build -t <ui-image> --label org.opencontainers.image.revision=<sha> ./frontend`
   - **Skipping this defers all image-build failure to the remote mutation
     window** (after the mandatory `pg_dump`). Recommend against `--skip-local-build`
     for a first cutover.

A failing gate aborts before any remote contact.

## C. Remote preflight (inside the locked session)

1. **lock** — `flock -n` on `~/.agents/deploy-runs/bean-counter/deploy.lock`.
2. **run-dir** — `~/.agents/deploy-runs/bean-counter/<stamp>-<short-sha>/`;
   write `deploy.lock.meta` and `local-preflight.txt`.
3. host facts — `hostname`, `uname -a`, `docker --version`,
   `docker compose version`, `docker ps`, `docker volume ls` → record.
4. **shared-DB dependency checks (fail-closed):**
   - the local-symphony `postgres` container exists and is `healthy`
     (`docker inspect`); bean-counter depends on a stack it does not own;
   - the external network `local-symphony_symphony-internal` exists;
   - the postgres volume `local-symphony_postgres-data` exists (so the backup
     target is real).
5. **beans version-parity gate (critical — see `04`):** read the live schema
   version with `... -p local-symphony ... exec -T postgres psql -U symphony -d
   symphony -t -A -c "select max(version_id) from bn_schema_versions;"` and
   compare to bean-counter's embedded max beans migration. **Abort if
   bean-counter's embedded max > the DB max** (it would migrate prod). Equal or
   lower is a no-op and proceeds. Abort if `bn_schema_versions` is missing.
6. secret present — `--dsn-secret` path is a readable file (presence only,
   never print contents). Optionally assert the DSN uses the `@postgres:5432`
   container host form (`assert_dsn_container_host`).
7. port free — `--ui-port` not already bound on the host.
8. remote checkout — exists, clean tracked tree; reject unexpected untracked
   files.
9. compose config render — `docker compose -p bean-counter -f
   deploy/docker-compose.prod.yml config` renders; confirm **no `db` service** is
   present; **scan the render for secret values** (`postgres://`, `password=`,
   token fields) and abort if present.

`--check` stops here.

## D. Backup (mandatory, before any mutation)

1. Record `previous_sha` (remote checkout HEAD) and previous image ids for the
   api and ui images (for rollback retagging).
2. **Mandatory consistent `pg_dump -Fc`** of database `symphony` (user
   `symphony`), run through the **local-symphony** compose project by service
   name (`docker compose -p local-symphony -f <base> -f <infra> --profile
   default exec -T postgres pg_dump -Fc -U symphony -d symphony > symphony.dump`),
   into the run-dir. Verify size > 0. If it fails or is empty, **abort before
   mutation** (no `--force`). This dump is the primary restore point. → `04`.
3. Best-effort forensic volume tarball of `local-symphony_postgres-data`
   (crash-consistent-at-best; non-fatal if it fails).
4. Generate `rollback.md` from captured state (retag previous images; data
   rollback documented, never auto-run; **never** `docker volume rm` the shared
   volume).

## E. Checkout & build

1. `git fetch origin` + `git checkout --detach <TARGET_SHA>`; assert HEAD ==
   target. Record `git-after.txt`.
2. Build images (skip if `--no-rebuild` and target SHA already deployed):
   - `docker build -t <api-image> .`
   - `docker build -t <ui-image> ./frontend`
   Record image ids.

## F. Compose up & health

1. `docker compose -p bean-counter -f deploy/docker-compose.prod.yml up -d api ui`
   using the **self-contained** prod compose (no `db` service at all — see `02`).
   Pin `-p bean-counter` so nothing here can resolve to the `local-symphony`
   project.
2. Wait for `api` health = `healthy` (readyz: verifies store + project) and
   `ui` health = `healthy` (nginx `/healthz`), deadline-driven.
3. Scan `api` startup logs (redacted) for store/DSN/migration failure
   signatures; abort on match.

## G. Smoke gate (replaces local-symphony's canary)

bean-counter has no orchestrator dispatch, so the success gate is a functional
HTTP smoke test against the **public** UI path (proves UI → api → shared DB):

1. `GET http://127.0.0.1:<ui-port>/healthz` → `200 ok`.
2. `GET http://127.0.0.1:<ui-port>/api/v1/readyz` → `200`.
3. `GET http://127.0.0.1:<ui-port>/api/v1/issues?limit=1` → `200`, **valid
   JSON** (an empty-but-valid array is a **pass** — do not gate on the tracker
   being non-empty, or an empty/quiet tracker fails a correct deploy).
4. Record (do not gate on) project-prefix evidence: log whether returned issue
   ids match `local-symphony-*`. The pass condition is "valid scoped response,"
   not "non-empty data."
5. **Non-mutating** smoke only — the deploy must not create/close real issues
   (full read/write is enabled for *users*, but the deploy script itself stays
   read-only against the shared DB beyond the backup).
6. **Known limitation:** a read-only smoke gate proves the read path but **not**
   the write path. Since the operator chose full read/write, write-path
   assurance comes from the version-parity gate + the optional staging
   dry-connect (task 11), which exercises CRUD against a throwaway DB — never
   against the `local-symphony` prefix in prod. State this gap explicitly.

A failed smoke gate aborts; the EXIT trap captures diagnostics and points at
`rollback.md`.

## H. Records & summary

Write to the run-dir: `summary.md` (refs, image ids, ports, smoke evidence,
backup path, version-parity result), `rollback.md`, `compose-config.yml`
(secret-scanned), `api-startup.txt` (redacted), `smoke.txt`. Print the run-dir
path at the end (`DEPLOY OK`).
