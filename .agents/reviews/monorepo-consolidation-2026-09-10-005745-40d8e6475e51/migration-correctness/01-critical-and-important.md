# Critical and Important Findings

## Critical

**None.** I found no security issue, data-loss path, crash, or broken functionality in
the migration-authored surface. In particular, the `ListFilter` adaptation — the highest
risk item in this branch — is exactly behavior-preserving. See
`03-positive-notes.md` for the verification.

---

## Important

### I1. Un-pinning the library raises the shipped beans schema version from 0008 to 0011, which will hard-fail the next production deploy, and nothing on this branch says so

**Severity:** Important (deploy-path functionality + missing documentation of a required
manual step)

**Files:**
- `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:356-369`
  (`resolve_embedded_migration_max`), `:504-511` (`--check` parity gate), `:677-688`
  (live parity gate)
- `/Users/punk1290/git/beans/apps/bean-counter/deploy/README.md:61-65`
- `/Users/punk1290/git/beans/.agents/plans/monorepo-consolidation/05-containers-and-deploy.md:278-300`

**Description**

Before this branch, `apps/bean-counter/go.mod` pinned
`github.com/mattsp1290/beans v0.1.2-0.20260615002029-e52dce57b52c`. At that commit the
library shipped eight Postgres migrations:

```
0001_bn_init … 0008_bn_dep_type
```

`libs/beans` HEAD ships eleven:

```
0009_bn_remote_url_unique.sql
0010_bn_issue_state_drop_check.sql
0011_bn_issue_repos_creation_commit.sql
```

(Verified with `git ls-tree --name-only e52dce57b52c:schema/migrations/postgres` against
`ls libs/beans/schema/migrations/postgres`; the same three are present in the `mysql` and
`sqlite` trees.)

Because the `replace` now resolves to the worktree, `resolve_embedded_migration_max`
computes `EMBEDDED_MAX=11` where the previously deployed image computed `8`. The parity
gate is:

```bash
if [ "$embedded_max" -gt "$db_max" ]; then
  echo "FAIL: bean-counter embedded migrations ($embedded_max) NEWER than prod ($db_max); would migrate shared schema" >&2
```

So the first deploy from this branch aborts with
`embedded migrations (11) NEWER than prod (8)` unless the shared local-symphony Postgres
has already been advanced to `0011` out of band with `bn`. There is deliberately no
`--force`.

The gate is doing its job — this is **not** a data-loss bug, and I want to be precise
about that: `store.New` calls `schema.Migrate`, so without the gate the API container
would silently apply `0009`–`0011` to a Postgres that bean-counter does not own,
including `ALTER TABLE bn_issues DROP CONSTRAINT IF EXISTS bn_issues_state_check`. The
gate prevents exactly that.

What is missing is the acknowledgement. `05-containers-and-deploy.md:291` describes the
change to `resolve_embedded_migration_max` as "a semantic improvement: the parity gate
now reads the migrations that are actually compiled into the image" and stops there. It
never states that the number those migrations produce jumps from 8 to 11, that the
consequence is a blocked deploy, or what the operator has to do first. `deploy/README.md`
still describes the gate abstractly. An operator following this branch's own docs will
hit a failed deploy with no documented remedy.

**Suggested fix**

Add the required pre-deploy step to `apps/bean-counter/deploy/README.md`, next to the
existing parity-gate paragraph:

```markdown
- **One-time migration step for the monorepo cutover.** Before the first deploy from
  the monorepo, the shared local-symphony Postgres must be advanced from beans schema
  0008 to 0011. bean-counter was previously pinned to beans
  v0.1.2-0.20260615002029-e52dce57b52c (max migration 0008); it now compiles against
  libs/beans HEAD (max migration 0011). Until the shared database is migrated, the
  schema-version parity gate will correctly abort every deploy with
  "embedded migrations (11) NEWER than prod (8)".

  Run the migration with the `bn` CLI against the shared DSN — bean-counter must not be
  the process that migrates a database it does not own:

      BN_DRIVER=postgres BN_DSN='<shared-symphony-dsn>' bn list --json --limit 1

  (Opening the store runs `schema.Migrate`.) Confirm with:

      select max(version_id) from bn_schema_versions;   -- expect 11

  Note that 0010 drops the `bn_issues_state_check` CHECK constraint and 0011 adds
  `creation_commit` to `bn_issue_repos`; review both against the local-symphony
  orchestrator's expectations before running them.
```

and file a follow-up bead for the actual database migration so it is tracked rather than
discovered at deploy time.
