# 04 — Safety, blast radius & rollback

bean-counter shares a database it does **not** own and is exposed
unauthenticated on the LAN with full read/write. Safety design is dominated by
that reality.

## Safety properties (carried from the reference)

- **Pinned target.** Deploy only a SHA reachable from `origin/main`; check out
  detached; never `git pull` after resolving the SHA (no TOCTOU race).
- **Single locked session.** One `flock`'d SSH session spans every mutating
  phase; concurrent deploys cannot interleave.
- **Backup before mutation.** Mandatory consistent `pg_dump -Fc` of database
  `symphony` (user `symphony`), run **through the local-symphony compose
  project** by service name (there is no container literally named `postgres` —
  the service has no `container_name`, so the real object is
  `local-symphony-postgres-1`):
  `docker compose -p local-symphony -f <base> -f <infra> --profile default exec -T postgres pg_dump -Fc -U symphony -d symphony > symphony.dump`
  Verify `symphony.dump` size > 0; abort if it fails or is empty. No `--force`.
- **Secret hygiene.** DSN read on the remote only; never on argv, never in the
  rendered compose config, never in the deploy record (render is secret-scanned;
  logs are redacted).
- **Fail-closed gates.** Any failed gate stops the deploy.

## Risk 1 (highest) — beans schema/version skew on the shared DB

`internal/store/adapter.go`: "beans/store.New owns schema migrations, so
bean-counter must not add its own beans-table migrations." `main.go` calls
`adapter.EnsureProject(ctx)` on startup, and `beansstore.New(...)` may run
migrations on connect. Pointing a bean-counter that pins
`beans v0.1.1` (`go.mod`) at the orchestrator's live schema risks:

- **Migration drift** — if bean-counter's beans version is *newer*, connecting
  could migrate the production schema in ways the running `bn`/orchestrator
  doesn't expect, breaking it.
- **Read/write incompatibility** — if *older*, bean-counter may fail or
  misread newer columns.

**The risk is asymmetric** (verified against beans source by review): `beans/store`
migrations are **goose forward-only**, version-tracked in the `bn_schema_versions`
table and guarded by a Postgres advisory lock. Therefore:

- bean-counter **older or equal** to prod (its embedded max migration ≤ the DB's
  current max) → `Up()` is a guaranteed **no-op**; it cannot downgrade. Safe.
- bean-counter **newer** than prod (embedded max > DB max) → it **will apply
  migrations** to the production schema on connect. This is the only dangerous
  direction.

Mitigations (all required before first prod connect):

1. **Version-parity preflight gate** (phase C5) — concrete, deterministic, needs
   no `bn` binary on the host:
   - read the live schema version:
     `docker compose -p local-symphony -f <base> -f <infra> --profile default exec -T postgres \`
     `psql -U symphony -d symphony -t -A -c "select max(version_id) from bn_schema_versions;"`
   - compare to bean-counter's embedded max beans migration (from the pinned
     `beans` module's `schema/migrations/postgres/` set).
   - **Abort if bean-counter's embedded max > the DB max.** Equal or lower
     proceeds (no-op migration). If the `bn_schema_versions` table is absent the
     DB is not a beans DB → abort.
2. **Pin bean-counter at or below the deployed beans version.** Never pin
   *above* prod — that would itself trigger the migration the gate prevents.
   (Prep task; edit `go.mod`/`go.sum`, re-run gates.)
3. **Mandatory pre-deploy `pg_dump`** as the restore point if a migration does
   fire unexpectedly.
4. **First deploy on a staging copy** if feasible: restore the dump into a
   throwaway **Postgres 16** DB (match prod; the local stack `db` is pg18) and
   point a bean-counter container at it to confirm `beansstore.New` (which calls
   `schema.Migrate`) + `EnsureProject` (an idempotent `INSERT ... ON CONFLICT DO
   NOTHING`) are no-ops before touching production. Recommended for the *first*
   cutover.

## Risk 2 — unauthenticated LAN read/write (operator-accepted)

Anyone on the LAN can open `http://10.0.0.106:8088` and create / close / delete
**real** orchestrator tracker issues. Accepted by the operator. Documented
mitigations to keep on the backlog (not blocking this deploy):

- Put the UI behind a reverse proxy with auth later (the rejected option from
  the intake question); the compose `ui` port can move to `127.0.0.1` and a
  proxy added without touching api.
- A future bean-counter read-only mode would shrink this blast radius.
- The pre-deploy backup bounds data loss to "since last deploy," not "forever."

## Risk 3 — operating on a stack we don't own

The Postgres lifecycle belongs to `local-symphony`. bean-counter's deploy and
rollback must treat it as read-mostly infrastructure:

- **Use a self-contained `deploy/docker-compose.prod.yml`** that defines ONLY
  `api` + `ui` — do **not** reuse `docker-compose.stack.yml`, which still
  defines a `db` service and a `bean-counter-postgres` named volume and wires
  `api.depends_on: db` (`service_healthy`). Reusing it risks Compose starting a
  stray Postgres. (If the stack file is layered for build defs, the prod file
  MUST override `api.depends_on` with `!reset []` and the secret scan/`config`
  render must confirm `db` is absent.)
- **Pin `-p bean-counter` on every compose invocation** — `up`, `config`, `ps`,
  `stop`, `down`, rollback alike — so a `down`/`-v` can never resolve to the
  `local-symphony` project. Inconsistent project names are the main footgun here.
- **Never** `docker volume rm` / `docker compose down -v` the shared volume
  (`local-symphony_postgres-data`).
- The backup `pg_dump` runs via the **local-symphony** compose project
  (`-p local-symphony ... exec postgres`), never `docker exec postgres` (no such
  container name), and writes only to the bean-counter run-dir.
- **Ordering dependency:** the api joins the external
  `local-symphony_symphony-internal` network, which exists only while the
  symphony stack is up. If symphony is recreated (`down`/`up`), that network is
  destroyed and bean-counter's api must be restarted. Document this; it survives
  symphony *restarts* but not *recreates*.

## Rollback

`rollback.md` is generated each run from captured previous state.

**Preferred — retag the previous images** (no data touched):

```bash
cd <repo-dir>
docker compose -p bean-counter -f deploy/docker-compose.prod.yml stop api ui
docker tag <previous_api_image_id> <api-image>
docker tag <previous_ui_image_id>  <ui-image>
docker compose -p bean-counter -f deploy/docker-compose.prod.yml up -d api ui
```

If a previous image id was not recorded, rebuild from `previous_sha` (weaker).
Do not prune images during a deploy.

**Stop bean-counter entirely** (back out the cutover; orchestrator untouched):

```bash
docker compose -p bean-counter -f deploy/docker-compose.prod.yml down
# (no -v: the shared Postgres volume is owned by local-symphony)
```

**Data rollback (manual, last resort — schema/data corruption only):** restore
`symphony.dump` into the shared DB. This affects the orchestrator too, so it is
**never** automated and requires coordinating a symphony stop first. The
generated `rollback.md` documents the exact `pg_restore` invocation but flags it
as a coordinated, manual operation.
