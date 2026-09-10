# Critical and Important

## Critical

**None.**

I looked for one. The three structural things a sixth pass should still worry
about on a branch like this — a deploy gate that can be defeated, history that
did not survive the import, and a build that only works in one mode — were each
attacked directly and each held. Details in the audit sections below.

## Important

**None that block the merge.**

The one issue I found is a stale number in the commit message, recorded under
Suggestions because the substantive claim around it is correct and because the
merge commit I am proposing is itself the natural place to correct it.

---

## Audit of `33890f4`'s commit message against the tree

Every previous round's message contained at least one overstatement. I checked
this one the same way — by re-running the experiments it reports rather than
reading it — in an isolated `git clone --local --no-hardlinks` of the repo, so
the working tree at `/Users/punk1290/git/beans` was never modified. The clone's
baseline is 62/62, matching the real repo.

### Claim: "the suite is 62/62 normally, with GIT_DIR set, with GIT_WORK_TREE set, and from an unrelated working directory"

**TRUE, and stronger than stated.** Six invocations, all 62 passed / 0 failed,
rc 0:

| Mode | Result |
|---|---|
| normal | 62/62 |
| `GIT_DIR=<canary>/.git` | 62/62 |
| `GIT_WORK_TREE=<canary>` | 62/62 |
| both set | 62/62 |
| from a non-git scratch directory | 62/62 |
| from `/` | 62/62 |

A passing count alone would not have proved the fix, because the round-4 bug
was precisely that the suite stayed green *while* the fixture cases went
vacuous. So I pointed `GIT_DIR`/`GIT_WORK_TREE` at a throwaway canary
repository with a known HEAD and checked it afterwards:

```
CANARY_BEFORE=97ef8a17b6c6359fe8a418d77ec9d55dcc3ddebd
CANARY_AFTER =97ef8a17b6c6359fe8a418d77ec9d55dcc3ddebd
CANARY_STATUS: 0 dirty entries
```

The canary was neither re-initialised nor dirtied, so the `unset GIT_DIR
GIT_WORK_TREE` at line 18 of the suite and the fixture's `env -u` are genuinely
redirecting nothing. This is the claim verified at the level the round-4 bug
demanded, not at the level of the exit code.

### Claim: "four of the nine entries … survive removal at 60/60"

**Substantively TRUE; the count is stale.** Dropping each of the nine
containment entries one at a time and re-running:

| Dropped entry | Result |
|---|---|
| `libs/beans` | 62 passed, 0 failed — **survives** |
| `libs/beans/schema` | 62 passed, 0 failed — **survives** |
| `libs/beans/schema/migrations` | 62 passed, 0 failed — **survives** |
| `libs/beans/schema/migrations/postgres` | 61 passed, 1 failed — covered |
| `apps/bean-counter` | 62 passed, 0 failed — **survives** |
| `apps/bean-counter/go.mod` | 61 passed, 1 failed — covered |
| `apps/bean-counter/Dockerfile` | 61 passed, 1 failed — covered |
| `apps/bean-counter/frontend` | 61 passed, 1 failed — covered |
| `"$COMPOSE_PROD"` | 61 passed, 1 failed — covered |

Exactly the four entries the message names survive; exactly the five it names
are individually covered. The message's own retraction of `faa3de8` is
therefore correct, and the redundancy explanation is correct too — the deeper
entries catch the same symlink because `require_in_repo` resolves physically.

The defect is the number: the suite is **62** tests after this round added two,
so those four survive at **62/62**, not 60/60. The figure was evidently measured
on the pre-fix 60-test suite and not restated after the new tests landed in the
same commit. Nothing about the finding changes; a reader who tries to reproduce
"60/60" simply will not, and may wrongly conclude the retraction was itself
wrong. Filed as a Suggestion, with the fix folded into the merge commit body.

### Claim: "deleting the loop fails six cases"

**TRUE, exactly.** Removing the whole `for rel in … require_in_repo … done`
block yields `56 passed, 6 failed`, and the six are the six victim cases:

```
FAIL - require_repo_root rejects a symlinked libs/beans
FAIL - require_repo_root rejects a symlinked libs/beans/schema/migrations/postgres
FAIL - require_repo_root rejects a symlinked apps/bean-counter/frontend
FAIL - require_repo_root rejects a symlinked apps/bean-counter/go.mod
FAIL - require_repo_root rejects a symlinked apps/bean-counter/Dockerfile
FAIL - require_repo_root rejects a symlinked apps/bean-counter/deploy/docker-compose.prod.yml
```

### Claim: "The new tracked-symlink check has its own case", and round 2's `pwd -P` fix "now has one"

**Both TRUE.** Two further mutations:

- Deleting the `tracked_links` block → `61 passed, 1 failed`, failing
  `require_repo_root rejects a TRACKED symlink not in the path list`.
- Reverting `here="$(pwd -P)"` to `here="$(pwd)"` → `61 passed, 1 failed`,
  failing `require_repo_root accepts a repo reached through a symlinked path`.

Both round-2 and round-5 fixes are now defended by tests that die when the fix
is reverted, which is the property that was missing.

### Claim: the structural check is live, and the invariant holds today

**TRUE.** `git ls-files -s -- libs/beans apps/bean-counter | awk '$1=="120000"'`
returns **zero** rows, and so does the same query over the entire repository.
There are no tracked symlinks anywhere.

### Claim: `"$COMPOSE_PROD"` is assigned once, not environment-overridable, reachable by no flag

**TRUE.** One assignment, a bare literal at line 66 with no `${…:-}` fallback:

```
66:COMPOSE_PROD="apps/bean-counter/deploy/docker-compose.prod.yml"
```

The other four occurrences (531, 1081, 1113, 1130) are reads. No flag writes it.

### Claim: `resolve_embedded_migration_max` has one caller two lines after `require_repo_root` and fatals under a reordering

**TRUE.** `require_repo_root` is called at line 1140 and
`resolve_embedded_migration_max` at 1142 — its only call site — and line 468
is `|| fatal "internal: REPO_ROOT_PHYS unset; require_repo_root must run first"`.

### Claim: "Verified: … `go list -m all` now leaves the tree clean"

**TRUE, reproduced end to end.** Deleting `go.work.sum`, running
`go list -m all`, and asking the exact question the deploy gate asks:

```
go.work.sum regenerated: 3215 bytes
git status --porcelain: (empty)
git check-ignore -v go.work.sum -> .gitignore:33:/go.work.sum
go.work      TRACKED
go.work.sum  NOT tracked
```

I then confirmed the other half of the round-5 rationale independently:
deleting `go.work.sum` and running `go work sync` does **not** regenerate it, so
the notes' "`go work sync` generates none for this workspace" is exact, and the
restored file is byte-identical to the one I found (`cmp` clean).

### Claim: "all three workflows parse", "the root fan-out resolves for all eight targets", "the deploy dry-run prints the monorepo paths"

**All TRUE.** All three workflows load under a real YAML parser; all eight
targets resolve under `make -n`, as do `fmt-check` and both escape hatches; the
dry-run exits 0 and prints `apps/bean-counter/deploy/docker-compose.prod.yml`,
`$HOME/git/beans` and `make -C apps/bean-counter test`.

### Verdict on the message

One stale number in eleven checkable claims, and the stale number sits inside
the paragraph that *retracts* a previous over-claim — so the substance is right
and only the arithmetic lagged. This is a materially different situation from
the previous five rounds, and it is not a reason to withhold approval.

---

## Things I attacked that held

Recorded so a future reader knows these were tested, not assumed.

- **A root-level `go build ./...` fails.** It reports `pattern ./...: directory
  prefix . does not contain modules listed in go.work`. I did **not** file this,
  because it is standard Go workspace behaviour, not a branch defect: I
  reproduced it in a clean two-module workspace built from scratch in a scratch
  directory, with no relation to this repo. The workspace root is not itself a
  module, so `./...` matches nothing. It matters only if something instructs
  people to run it — and nothing does. `README.md`, `AGENTS.md` and `CLAUDE.md`
  all steer to `make ci` / `make build` or the per-module `GOWORK=off` form, and
  `ci-workspace.yml` uses the correct `go build ./libs/beans/...
  ./apps/bean-counter/...`. No finding.
- **`make ci` leaving artefacts behind.** It writes `libs/beans/bin/bn` and
  `apps/bean-counter/bin/bean-counter`, and `tidy-check` rewrites `go.mod`/
  `go.sum` in place before diffing them. `git status --porcelain` is empty
  afterwards, so `bin/` is covered by the ignore rule and `tidy` is a true
  no-op. No finding.
- **`shellcheck` on the repo's other two shell scripts.** `setup-beads.sh` and
  `setup-multi-repo-beads.sh` emit info-level SC2016/SC2086. The counts are
  **identical to `main`** (4 and 100), the branch only appends 6 lines to each,
  and CI's shellcheck job scopes to the two deploy scripts by design. Not a
  regression, not in scope. No finding.
- **The `GOWORK=off` path actually using the local library.** `GOWORK=off go
  list -m` inside `apps/bean-counter` returns `v0.0.0 => ../../libs/beans`, so
  the replace directive — not a published version — is what resolves. No
  finding.
