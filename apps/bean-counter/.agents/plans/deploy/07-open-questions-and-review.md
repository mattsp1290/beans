# 07 — Open questions, risks & review reconciliation

## Resolved by operator (intake)

- **DB topology:** share the production beans DB (`BN_PROJECT_PREFIX=local-symphony`).
- **Exposure:** LAN port (`0.0.0.0:8088`), no auth.
- **Write posture:** full read/write; backup + rollback are the safety net.

## Open questions for the reviewers / operator

1. **External network name.** Plan assumes `local-symphony_symphony-internal`.
   Must be confirmed on the host (`docker network ls`); if local-symphony was
   started with a custom project name, this differs. Fallback: `127.0.0.1:5432`
   + DSN host rewrite.
2. **DSN secret ownership.** Reuse `infra-admin`'s `symphony-secrets/bn_dsn`, or
   provision a dedicated `bean-counter` secret? Reuse is simplest; a dedicated
   secret is cleaner separation.
3. **`BN_DSN_FILE` now vs stopgap.** Add file-based DSN support before first
   deploy (recommended) or ship with the option-2 export stopgap and follow up?
4. **Staging dry-connect (task 11).** Worth doing for the first cutover to prove
   migrations are no-ops, or accept the version-parity gate + backup alone?
5. **Compose `depends_on` on `db`.** _Resolved (rev 2):_ prod uses a
   self-contained `deploy/docker-compose.prod.yml` with no `db` service at all;
   `stack.yml` is not layered in. Verify with `docker compose -p bean-counter -f
   deploy/docker-compose.prod.yml config` that `db` is absent.
6. **`-p bean-counter` project scoping.** _Resolved (rev 2):_ every compose
   invocation pins `-p bean-counter`; the backup `pg_dump` uses `-p
   local-symphony`. Confirm the helper enforces this uniformly.

## Risk register (see 04 for detail)

| # | Risk | Severity | Mitigation |
|---|------|----------|-----------|
| 1 | beans schema/version skew migrates/breaks the shared prod DB | **Critical** | version-parity gate, pin beans, mandatory pg_dump, optional staging dry-connect |
| 2 | unauthenticated LAN read/write to real tracker | High (accepted) | backup; future proxy+auth / read-only mode |
| 3 | acting on the local-symphony-owned stack | High | never `-v`/`down` the shared volume/project; scope to `-p bean-counter` |
| 4 | secret leak via rendered compose / logs | Medium | `BN_DSN_FILE`, secret-scan render, redact logs, no argv |
| 5 | port 8088 conflict | Low | preflight port-free check; `--ui-port` override |
| 6 | wrong/dirty remote checkout deployed | Medium | clean-tree gate, detached checkout at pinned SHA, no `git pull` |

## Grounding notes (verified against source)

- API routes: `GET /api/v1/healthz` (`internal/server/app.go:39`, liveness),
  `GET /api/v1/readyz` (`internal/handlers/health/health.go:20`, readiness —
  checks store + project via `ProjectExists`). **Distinct** from the data route
  `GET /api/v1/ready` (`internal/handlers/ready/ready.go:23`, the ready *issue
  queue*) — do not confuse them when wiring healthchecks. Other data routes:
  `/api/v1/{issues,deps,graph}`. UI liveness `GET /healthz`
  (`frontend/nginx.conf`). The `docker-compose.stack.yml` healthcheck on
  `readyz` is **correct** (route exists) — no mismatch.
- beans migrations are **goose forward-only**, tracked in `bn_schema_versions`,
  advisory-locked; `beansstore.New` calls `schema.Migrate` on connect;
  `EnsureProject` is `INSERT ... ON CONFLICT DO NOTHING` (idempotent). So the
  version-skew risk is one-directional (bean-counter newer than prod).
- Shared DB identity: database `symphony`, user `symphony` (local-symphony
  `docker-compose.yml`). The postgres service has **no `container_name`**, so the
  real object is `local-symphony-postgres-1` — address it by compose service
  name, never `docker exec postgres`.
- `BN_*` config surface: `BN_DRIVER, BN_DSN, BN_PROJECT_PREFIX, BN_ACTOR,
  BN_CORS_ORIGIN, BN_ADDR, BN_MAX_CONNS, BN_MIN_CONNS, BN_CONNECT_TIMEOUT`
  (`internal/config/config.go`). **`BN_DSN_FILE` does not exist yet** — it is a
  prep task.
- `beans/store` owns migrations; bean-counter must not add its own
  (`internal/store/adapter.go`). `main.go` calls `adapter.EnsureProject`.
- bean-counter pins `github.com/mattsp1290/beans v0.1.1` (`go.mod`).
- No `scripts/` dir exists yet; `deploy/` does not exist in bean-counter (it
  does in local-symphony — the reference). `.github/` exists.

## Reviewer reconciliation log

Two independent Opus reviewers (A: SRE/infra; B: staff eng) assessed this plan.
Both verified their claims against source. All findings below were **accepted**
and folded into the plan (revision 2); none rejected. Reconciliation discipline
(per review-workflow): prefer the stricter severity; verify symbols against
source; record a reason per rejected finding.

| # | Finding | Reviewer(s) | Severity | Decision | Where applied |
|---|---------|-------------|----------|----------|---------------|
| 1 | `pg_dump` under-specified (DB/user/container) | A, B | BLOCKER | Accept | `03`§D.2, `04` Backup — exact `-p local-symphony exec postgres pg_dump -U symphony -d symphony`, size>0 check |
| 2 | `-p bean-counter` scoping inconsistent; `stack.yml` `db`/volume + `depends_on:db` could start stray Postgres | A, B | BLOCKER | Accept | `02` (self-contained prod compose, `!reset []`), `03`§F.1, `04` Risk 3, rollback cmds |
| 3 | Version-parity gate had no mechanism | A, B | BLOCKER | Accept | `03`§C.5, `04` Risk 1 — `select max(version_id) from bn_schema_versions` vs embedded max |
| 4 | Risk is asymmetric (only bean-counter-newer is dangerous); don't pin above prod | A | MAJOR | Accept | `04` Risk 1, `06` tasks 1–2 |
| 5 | Secret unreadable by non-root container uid (api runs `bean-counter`) | A | MAJOR | Accept | `02` DSN note — container-uid readability preflight |
| 6 | Local gates mismatch reality (e2e suite, `make` targets, testcontainers need Docker) | B | MAJOR | Accept | `03`§B |
| 7 | `/readyz` vs `/ready` route conflation | B | MAJOR | Accept | `05`, `07` grounding |
| 8 | Smoke gate doesn't prove write path; empty tracker → false failure | A, B | MAJOR/MINOR | Accept | `03`§G (valid-but-empty = pass; write-path gap stated) |
| 9 | `--no-rebuild` provenance by SHA label, not moving tag | B | MAJOR | Accept | `02` `--no-rebuild` note, `03`§B.5 build labels |
| 10 | External network requires symphony up; lost on recreate | A | MAJOR | Accept | `04` Risk 3 ordering dependency |
| 11 | flag→env-var mapping table; `--dsn-secret` path derivation | A, B | MINOR/NIT | Accept | `02` mapping table + DSN note |
| 12 | pg16 (not pg18) for rollback/staging rehearsal | A | MINOR | Accept | `04` Risk 1.4, `05` |
| 13 | `--skip-local-build` defers failure to remote window | B | MINOR | Accept | `03`§B.5 |
| 14 | CI already exists — extend, don't create | B | NIT | Accept | `05`, `06` task 14 |

Confirmed by reviewers (no action): `TestStoreDSNFormattingIsRedacted` exists
(`config_test.go:111`); `go.mod` has no `beans` `replace`; `make` targets and
npm scripts exist; `EnsureProject` is a safe idempotent insert; reads are
read-only; once #1/#2 are fixed, nothing in the deploy breaks the orchestrator.
