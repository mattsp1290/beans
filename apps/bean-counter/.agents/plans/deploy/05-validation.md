# 05 — Validation

Two layers: unit tests for the script's pure logic, and end-to-end deploy
verification against the host.

## Script unit tests — `test/scripts/deploy-production_test.sh`

Mirror `local-symphony/test/scripts/update-production_test.sh`: `source` the
deploy script (guarded by `BASH_SOURCE == $0`) and exercise the pure helpers
without launching a deploy.

- `normalize_ui_port` — accepts valid ports; rejects non-numeric / out-of-range.
- `assert_dsn_container_host` — accepts `...@postgres:5432/...`; rejects host
  forms incompatible with the external-network design (fail-closed).
- `extract_issue_count` — parses the smoke JSON; fails on malformed input.
- arg parsing — `--ref` required; `--force` / unknown flags rejected; `--check`
  / `--dry-run` set the right mode.

Hermetic by default; a `RUN_NETWORK_TESTS=1` path may additionally check
`origin` resolution and SSH reachability, exactly like the reference.

CI already exists (`.github/workflows/ci.yml` runs backend fmt/vet/lint/test/build,
frontend test/check/build, and a matrix integration job). The new work is to
**extend** that workflow with a `shellcheck` + script-unit-test job, reusing the
established job structure — not to create CI.

## Pre-merge gates

- `shellcheck scripts/deploy-production.sh test/scripts/deploy-production_test.sh`
- `bash test/scripts/deploy-production_test.sh`
- existing repo gates unaffected: `go test ./...`, `make lint`, frontend
  `npm run check` / `npm test`.
- if `BN_DSN_FILE` support is added: unit test in `internal/config` that a DSN
  read from a file is loaded and is **redacted** in formatted/JSON output
  (extend the existing `TestStoreDSNFormattingIsRedacted` pattern).

## Deploy dry-run & check (no mutation)

```bash
scripts/deploy-production.sh --ref main --dry-run   # resolve SHA, print plan, SSH ping
scripts/deploy-production.sh --ref main --check     # remote preflight, read-only
```

`--check` must confirm: docker present; shared `postgres` container healthy;
external network present; postgres volume present; DSN secret readable; UI port
free; remote checkout clean; compose config renders and is secret-free; beans
version parity OK.

## End-to-end verification (first live deploy)

1. Run live deploy of `origin/main`; expect `DEPLOY OK` and a run-dir path.
2. From a workstation on the LAN: open `http://10.0.0.106:8088` — UI loads.
3. `curl http://10.0.0.106:8088/api/v1/readyz` → `200`.
4. `curl 'http://10.0.0.106:8088/api/v1/issues?limit=5'` → returns
   `local-symphony-*` issues (proves shared-DB + correct prefix).
5. `curl http://10.0.0.106:8088/api/v1/graph` → `200`, renders deps.
6. Confirm the orchestrator is **unharmed**: the symphony container is still
   running/healthy and the `bn ready` count on the host is unchanged from before
   the deploy (the deploy must not have mutated tracker data).
7. Inspect the run-dir: `summary.md`, `rollback.md`, `pg_dump` present,
   `compose-config.yml` contains no `postgres://`.

## Rollback rehearsal

Once, in a maintenance window: deploy, then run the `rollback.md` image-retag
path and confirm the previous bean-counter version comes back healthy and the
orchestrator is untouched. Proves the rollback doc is correct before it is ever
needed in anger.

**Use Postgres 16 for any rollback/staging rehearsal** (the prod shared DB is
`postgres:16-alpine`; the local `docker-compose.stack.yml` `db` is
`postgres:18-alpine`). `pg_dump`/`pg_restore` behavior differs across major
versions, so a pg18 rehearsal is not representative of restoring the pg16 prod
dump.

> Endpoint note for implementers: `/api/v1/readyz` (readiness,
> `health.Register`) and `/api/v1/ready` (data — the ready *issue queue*,
> `ready.Register`) are **different** endpoints. The healthcheck and smoke gate
> use `/readyz`; do not wire the container healthcheck to the data route.
