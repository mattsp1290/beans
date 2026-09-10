# Critical and Important

## Critical

None. Every executable gate passes, the tree is clean, the branch is a strict
descendant of `main`, and no shipped code, workflow, Dockerfile, compose file or
shell script is wrong that I could find.

---

## Important

### I1 — A stale `bd` memory put a wrong schema baseline into an open P0 and into a commit message

**Where:** `bd` memory `bean-counter-prod-10-0-0-106-beans`; open P0 issue
`beans-ued`; commit `40d8e64` message body.

The memory reads:

> bean-counter prod (10.0.0.106) beans schema is at version 8 (bn_schema_versions
> max=8); **bean-counter pins beans v0.1.1 which embeds only through migration
> 0007.** Deploy parity gate passes (older is safe) …

That was true until **2026-06-14**, when commit `df029e9` ("Upgrade beans to
e52dce57b52c (matches production)") bumped the pin and said so in its own message:

> Bumps github.com/mattsp1290/beans v0.1.1 -> v0.1.2-0.20260615002029-e52dce57b52c
> … This raises bean-counter's embedded beans schema to migration 0008 (= prod),
> **resolving the deploy parity finding (was embedded 7 vs prod 8).**

I confirmed this from the tree rather than from either message:

- `git show 6895dea:apps/bean-counter/go.mod` — the last pre-rename state — requires
  `github.com/mattsp1290/beans v0.1.2-0.20260615002029-e52dce57b52c`.
- That version in the module cache
  (`.../beans@v0.1.2-0.20260615002029-e52dce57b52c/schema/migrations/postgres/`)
  contains exactly `0001` … `0008`. The `v0.1.1` cache entry stops at `0007`.

So the pre-monorepo baseline was **0008, exactly level with prod's 0008** — not 0007
behind it. The migration never re-derived this; it copied the stale memory forward:

- **`beans-ued` (P0, open)** — "bean-counter pinned beans **v0.1.1**, embedding
  through **0007** - older than prod, so the deploy parity gate PASSED."
- **`40d8e64`** — "raises the embedded beans migration max **from 7 to 11** against a
  production database at 8".

Meanwhile `apps/bean-counter/deploy/README.md` and
`.agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md` both state the correct
figure (`0008` embedded against a database at `0008`, "the gate passed"). The
repository now carries two contradictory accounts of the same fact, and the wrong one
is in the P0 that gates a migration of a **shared production Postgres this project
does not own**.

Why it matters beyond arithmetic: the two versions imply different prior states.
"7 against 8" means bean-counter was *behind* prod and had headroom. "8 against 8"
means it was *exactly level* — there was no slack at all, and the jump to 11 is the
first time this application has ever proposed advancing that database. An operator
weighing `beans-ued`'s two options (advance the shared DB to 0011 with local-symphony's
owner, versus pin back to a library commit at or below the DB's version) should be
reading the second framing, especially since `0010_bn_issue_state_drop_check.sql`
drops a CHECK constraint.

The memory is the root cause and the thing future agents will read first.

**Fix:** `bd remember` the corrected fact (prod at 8; bean-counter pinned
`v0.1.2-0.20260615002029-e52dce57b52c` embedding through 0008 — exact parity; the
monorepo raises embedded to 11), then `bd update beans-ued` to match. The `40d8e64`
commit message is already published history and should be left alone; the correction
belongs in the merge commit note, the way `e3875a8` corrected `5bd9e07`.

**Not merge-blocking.** It is tracker state, which git does not carry (see 00,
"Merge instructions"). It *is* deploy-blocking.

---

### I2 — Four open deploy issues still name paths this migration invalidated

**Where:** `bean-counter-m0p` (P0), `bean-counter-am5` (P0), `bean-counter-mkg` (P1,
epic), `bean-counter-log` (P1) — all open.

`bean-counter-m0p`'s description, in full:

> Run `scripts/deploy-production.sh --ref main`; verify per
> **`.agents/plans/deploy/05-validation.md`**; confirm orchestrator unharmed.

Neither path exists. `6895dea` hoisted `.agents/plans/deploy/` to
`.agents/plans/bean-counter-deploy/`, and the script now lives at
`apps/bean-counter/scripts/deploy-production.sh`. This is the *same dead directory*
that `e3875a8` singled out and fixed in `apps/bean-counter/deploy/README.md`:

> apps/bean-counter/deploy/README.md linked ../.agents/plans/deploy/, which this
> migration hoisted to .agents/plans/bean-counter-deploy/. It was the only broken
> relative link on the branch …

It was the only broken link *in a file*. The identical dead path in the P0 issue that
tells an operator how to validate a production deploy went unnoticed, because the
sweep (`94da2fa`) scoped itself to files and the tracker was consolidated separately
(`4af7ed0`).

Scanning the migration's own committed export
(`.agents/plans/monorepo-consolidation/bean-counter-issues.jsonl`, 55 records) for
pre-monorepo paths turns up six records, four of them open:

| Issue | Status | Pri | Stale reference |
|---|---|---|---|
| `bean-counter-m0p` | open | P0 | `.agents/plans/deploy/05-validation.md`; unprefixed `scripts/deploy-production.sh` |
| `bean-counter-am5` | open | P0 | unprefixed `scripts/deploy-production.sh` |
| `bean-counter-mkg` | open | P1 | `.agents/plans/deploy/`; unprefixed `scripts/deploy-production.sh` |
| `bean-counter-log` | open | P1 | "Bootstrap remote bean-counter checkout" at `git/bean-counter` — superseded by `beans-nlc`, which now owns the move to `git/beans` |
| `bean-counter-gyf` | closed | P0 | historical record — correctly left alone |
| `bean-counter-67k` | closed | P0 | historical record — correctly left alone |

**On whether this was a departure.** Plan `08-execution-handoff.md` says:

> `bean-counter-m0p`, `bean-counter-log`, and `bean-counter-am5` carry over from the
> bean-counter tracker unchanged. They describe the first production deploy and are
> consumers of this migration, not part of it.

So "unchanged" *was* the plan, and I am not calling this silently dropped. But that
decision was written before the migration hoisted the plan directory, repointed the
script, and changed the deploy script's default `--repo-dir` from
`$HOME/git/bean-counter` to `$HOME/git/beans`. The migration's own later work is what
falsified those descriptions. `IMPLEMENTATION-NOTES.md` — whose stated job is
recording where implementation departed from the plan so "a reader of the plan is not
misled by a criterion that no longer describes the repository" — does not mention it.
That omission is the finding.

**Fix:** `bd update` the four open issues to name the current paths, and add a short
paragraph to `IMPLEMENTATION-NOTES.md` recording that plan 08's "carry over
unchanged" no longer holds for path references, and why.

**Not merge-blocking**, for the same reason as I1.

---

### I3 — `deploy/README.md`'s "before the first post-monorepo deploy" section covers only one of the two blockers

**Where:** `/Users/punk1290/git/beans/apps/bean-counter/deploy/README.md`, the
`## Before the first post-monorepo deploy: schema parity` section (line 98) and the
`## Rollback` block (line 135).

The section is thorough about the schema-parity blocker: it names the exact three
migrations, quotes the abort message, gives two resolution paths, and says not to
weaken the gate. It says nothing about the *other* prerequisite this branch created.

`da07354` changed `DEFAULT_REPO_DIR` to `'$HOME/git/beans'`. Per `beans-nlc` (P0,
open), the infra host's live stack is still checked out at
`/home/infra-admin/git/bean-counter`, with `bean-counter-api-1` and
`bean-counter-ui-1` healthy and up ~2 months. That move has **not** been performed
and needs explicit approval. Yet this README's Rollback recipe opens with:

```bash
cd ~/git/beans
```

— a directory that does not exist on that host today. An operator following the
document titled "Before the first post-monorepo deploy" gets a complete account of
one blocker and no hint of the other, on a production host this repository does not
own.

**Mitigating, and why this is Important rather than blocking:** the dependency graph
already enforces the ordering. `bean-counter-m0p` depends on `beans-nlc`,
`beans-ued`, `bean-counter-am5` and `bean-counter-log`, so `bd ready` will not
surface the deploy until the checkout move closes. And the script itself fails safe —
the dry-run only tests SSH connectivity and mutates nothing. The gap is in the
narrative document, not in the machinery.

**Fix:** four lines in that section — the remote checkout is still
`$HOME/git/bean-counter`, the script now defaults to `$HOME/git/beans`, `beans-nlc`
owns the move, and it must close before any `--check` or live run. This is the one
finding of the three that lives in the git tree and could be fixed in the branch.
