# 03 — Validation & rollback

## Pre-merge gates (must all pass)

- `git diff go.mod go.sum` shows **only** the beans bump (+ its go.sum hashes)
  and, under the recommended approach, small handler/adapter edits — no
  unrelated dependency churn.
- `go build ./...`
- `go test ./...` (incl. `test/e2e` sqlite smoke)
- `go test -tags=integration ./...` (postgres + mysql via testcontainers; needs
  a local Docker daemon). **This is a manual/local gate — CI does NOT run it:**
  `.github/workflows/ci.yml` `backend` runs `make test` (no `integration` tag),
  and the `deploy-scripts` job is unrelated. Since the dep-edge behavior change
  touches the postgres/mysql SQL path, run integration **locally before merge**.
- `make vet lint fmt-check`
- frontend: `npm ci && npm run check && npm test && npm run build`
- new tests from `02` step 5 pass and actually exercise the new behavior.

## Behavioral verification (the point of the upgrade)

1. **Embedded schema version is 8.** Quick check:
   ```bash
   ls "$(go list -m -f '{{.Dir}}' github.com/mattsp1290/beans)/schema/migrations/postgres" | tail -1
   # -> 0008_bn_dep_type.sql
   ```
2. **Deps/graph stay blocking-only** (recommended approach) — covered by the new
   sqlite test; a parent-child edge must not appear in `/deps` or `/graph`.
3. **Ready excludes epics** — covered by the new test.
4. **Deploy parity** — `scripts/deploy-production.sh --ref <sha> --check` prints
   `embedded_max=8 db_max=8` and `CHECK OK`. **Sequencing caveats (review):**
   this is a **post-merge** check — the script requires `<sha>` reachable from
   `origin/main` (`deploy-production.sh:281`), so it cannot run on the feature
   branch and is **not** a pre-merge gate. It also needs the deploy script
   itself (currently only on `fix/deploy-parity-stdin`, not yet on `main`) and
   operator Docker access to the prod host. Treat it as an operator step gated on
   the deploy-parity PR landing first.

## Production-parity confirmation

The target commit (`e52dce5`) is the exact build prod runs. After the bump,
bean-counter and local-symphony resolve the **same** beans module
(`v0.1.2-0.20260615002029-e52dce57b52c`). Confirm:
```bash
grep beans go.mod          # matches local-symphony's pin
```

## Rollback

Pure dependency + small-handler change, fully reversible in git:

- **Before merge:** `git checkout -- go.mod go.sum internal/…` (or drop the
  branch). No deployed state involved.
- **After a bean-counter deploy on the bumped version:** roll back via the
  deploy harness `rollback.md` (retag previous images). The bump does **not**
  change the shared DB (prod already at schema 8), so there is **no schema
  rollback** to perform — this is the key safety property of matching prod
  rather than leading it.

## What this upgrade explicitly does NOT do

- It does not migrate or mutate the production database (prod is already at
  schema 8; `beansstore.New` `Up()` is a no-op there).
- It does not change the public API response shape (recommended approach).
- It does not add epic/membership UI — that is a separate feature.
