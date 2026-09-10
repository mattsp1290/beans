# 06 — Task sequence (beads-ready)

Ordered, dependency-aware. Labels: `prep`, `impl`, `testing`, `docs`,
`deploy`. Priorities: P0 critical, P1 high, P2 medium. File these as beads
issues (`bd create`) once the plan is approved; suggested deps noted.

## Phase 0 — prerequisites (unblock the shared-DB design)

1. **[P0, prep] Establish beans schema parity with production.** Read
   `max(version_id)` from `bn_schema_versions` in the shared DB (via the
   `local-symphony` postgres service) and compare to bean-counter's embedded max
   beans migration. Record findings. The gate is asymmetric: only
   bean-counter > prod is dangerous. Blocks everything that connects to the
   shared DB.
2. **[P0, prep] Pin bean-counter's `beans` dependency at or below the prod
   version** if they differ (edit `go.mod`/`go.sum`, re-run gates). Never pin
   *above* prod (it would trigger a migration). Depends on (1).
3. **[P1, impl] Add `BN_DSN_FILE` support to `internal/config`.** If set, load
   the DSN from the file path; keep redaction guarantees. Unit test it. Enables
   secret-safe compose. (If deferred, use the option-2 stopgap from `01`.)

## Phase 1 — deploy artifacts

4. **[P1, impl] `deploy/docker-compose.prod.yml`** — api + ui, no db, external
   `local-symphony_symphony-internal` network, secret mount, UI port. Verify the
   external network name on the host and that `db` is not pulled in via
   `depends_on`. Depends on (3) for `BN_DSN_FILE`.
5. **[P0, impl] `scripts/deploy-production.sh`** — full contract from `02`:
   SHA pinning, local gates, locked remote session, preflight (incl. version
   parity + port-free + shared-DB health), mandatory `pg_dump`, checkout/build,
   compose up api+ui, health, smoke gate, records, generated `rollback.md`,
   no `--force`. Depends on (4).
6. **[P1, testing] `test/scripts/deploy-production_test.sh`** — pure-helper and
   arg-parsing unit tests; shellcheck clean. Depends on (5).
7. **[P2, docs] `deploy/README.md`** — operator runbook (dry-run/check/live,
   rollback, secret model, blast-radius warning). Depends on (5).

## Phase 2 — host bootstrap & first cutover

8. **[P1, deploy] Bootstrap remote checkout** — clone bean-counter to
   `infra-admin@10.0.0.106:~/git/bean-counter`; confirm SSH BatchMode works.
9. **[P0, deploy] Decide & wire the DSN secret** — reuse
   `/home/infra-admin/symphony-secrets/bn_dsn` or provision a dedicated
   `bean-counter` secret; confirm readable, container-host DSN form. Depends on (8).
10. **[P1, testing] `--check` against the host** — green preflight. Depends on
    (5),(8),(9).
11. **[P2, deploy] (Recommended) staging dry-connect** — restore the `pg_dump`
    into a throwaway DB, point a bean-counter container at it, confirm
    `beansstore.New` + `EnsureProject` are no-ops. Depends on (1),(5).
12. **[P0, deploy] First live deploy** — `scripts/deploy-production.sh --ref
    main`; verify per `05`; confirm orchestrator unharmed. Depends on (10), and
    (11) if performed.
13. **[P2, deploy] Rollback rehearsal** — exercise `rollback.md` image-retag in
    a window. Depends on (12).

## Phase 3 — follow-ups (backlog, non-blocking)

14. **[P2, impl] Extend existing CI** (`.github/workflows/ci.yml`) with a
    shellcheck + script-unit-test job (CI already exists; do not recreate it).
15. **[P3, impl] bean-counter read-only mode** — guard mutating endpoints; would
    shrink Risk 2 blast radius.
16. **[P3, impl] Reverse proxy + auth** — the deferred exposure hardening.

## Critical path

(1)→(2)→(3)→(4)→(5)→(10)→(12). Items (6),(7),(11),(13) gate quality/safety
around it; Phase 3 is backlog.
