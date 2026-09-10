# Positive notes

### The three items from pass 4 are fixed in the records, not just in prose

I checked the artifacts themselves rather than the commit's description of them.

**The number.** `bd memories --json` returns:

> "…measured 2026-06-14. CORRECTED 2026-09-10: an earlier version of this memory said
> bean-counter 'pins beans v0.1.1 which embeds only through 0007'. That was already stale
> when written — commit df029e9 had upgraded the dependency to the pseudo-version
> v0.1.2-0.20260615002029-e52dce57b52c, whose commit e52dce5 embeds through 0008 (verified
> with git ls-tree). So embedded and prod were EQUAL at 8, not 7 vs 8."

I verified the underlying fact independently: `git ls-tree -r e52dce5` lists
`schema/migrations/postgres/0001…0008`, and the current tree carries `0001…0011`. The
deploy dry-run reports `embedded_max: 11`. `beans-ued` now carries the same corrected
account with the same evidence, and flags the memory's old claim as stale rather than
silently overwriting the history of the error. Correcting the source of a wrong number, the
issue that copied it, and leaving the original commit message intact with the correction
recorded forward is the right handling — an amended commit message would have hidden that
the two accounts ever disagreed.

**The dead paths.** I scripted a scan over all 17 open issues from `bd list --status=open
--json`. Zero hits for `.agents/plans/deploy/` and zero for an unprefixed
`scripts/deploy-production.sh`. Every surviving reference resolves:
`bean-counter-m0p` → `.agents/plans/bean-counter-deploy/05-validation.md` (exists),
`bean-counter-mkg` → `.agents/plans/bean-counter-deploy/` (exists, 8 documents),
`-m0p`/`-am5` → `./apps/bean-counter/scripts/deploy-production.sh` (exists). Neither
`.agents/plans/deploy/` nor a root `scripts/` directory exists any more, so the check has
teeth. The remaining `git/bean-counter` mentions are all deliberate — they describe the old
checkout that `beans-nlc` and `beans-a98` exist to retire.

**The second blocker.** `apps/bean-counter/deploy/README.md` now has a
"Before the first post-monorepo deploy: the remote checkout" section next to the
schema-parity one, and it closes with "both must close before `bean-counter-m0p` can
proceed." It also states the parity history correctly ("embedding through `0008` against a
production database at `0008` — the gate passed"), so the operator-facing document and the
tracker now agree on the number that was wrong two rounds ago.

### The history import is real, and it is verifiable

`git rev-list --count bfe40cc^2` is exactly 114, rooted at bean-counter's own initial commit
`dd88be7`. Two root commits are reachable from HEAD. `git log --follow --
libs/beans/store/store.go` walks 41 commits back through the `store/` → `libs/beans/store/`
rename to `39d689c "Extract bn into beans module"` (2026-06-13), and `git log --
apps/bean-counter/internal/server/app.go` reaches `433d900 "ralph: iteration 1 checkpoint -
initialize Go Fiber skeleton"` (2026-06-14) at the rewritten path. Success criteria 4 and 5
are met with margin, and the 15 first-parent commits make the monorepo work itself easy to
read as a sequence.

### The deploy script's containment control is genuinely hard to get past

Independently of the coverage claim in finding I1, I probed `require_in_repo` at HEAD with
18 input shapes against a scratch fixture. Every out-of-tree shape was rejected: a plain
out-of-tree symlink, the same with a trailing slash and with `/.`, an in-tree symlink, a
dangling symlink, `../../etc/passwd`, `/etc/passwd`, `//etc/passwd`, the empty string, `.`,
`..`, the repository root itself, a glob (`lib*`), bracket characters (`lib[s]/beans`), an
embedded newline, a doubled slash *through* an out-of-tree symlink (`evil//sub`), and an
out-of-tree FIFO reached by symlink. Only genuinely in-repo paths were accepted — including
an in-repo doubled slash (`libs//beans`) and an in-repo FIFO, which is correct: this is a
containment check, not a file-type check.

The `resolve_embedded_migration_max` fix is also real. It now opens with

```
  [ -n "$REPO_ROOT_PHYS" ] \
    || fatal "internal: REPO_ROOT_PHYS unset; require_repo_root must run first"
  root_phys="$REPO_ROOT_PHYS"
```

which closes the swallowed-`git`-failure path the commit describes, and it resolves the
migrations directory itself rather than only the module directory — so a symlink at
`schema`, `migrations` or `postgres` cannot source the parity count from outside the tree.

### The gates are the honest kind

Three details stood out as written by someone who expected to be checked:

- `apps/bean-counter`'s `fmt-check` refuses to pass vacuously:
  `test -n "$files" || { echo "fmt-check found no Go files; refusing to pass" >&2; exit 1; }`.
- The `images` job probes the **named build stage**, not the runtime image, and asserts a
  known-present file first so a broken `docker run` cannot read as a pass:
  `test -f /src/libs/beans/go.mod || { echo "probe is broken…"; exit 1; }` before checking
  that `/src/go.work` is absent. That is the single most important unverifiable-locally
  property on this branch — that the image resolves the library through the `go.mod`
  `replace` alone — and CI proves it rather than asserting it.
- `ci-workspace.yml` is deliberately unfiltered, with the reason in a comment, so a change
  anywhere still proves the workspace coheres. Its sync step checks `git status
  --porcelain` on top of `git diff --exit-code`, with a comment explaining that `git diff`
  only sees tracked files.

`ci-libs-beans.yml` pinning `GOWORK: 'off'` at the job level, so the library is proven to
stand alone rather than being propped up by the workspace, is the right default for a
`libs/` module in a monorepo.

### The `docker` outage is handled the way it should be

Three success criteria and `make ci-integration` cannot be verified on this machine
(`docker info` → `Cannot connect to the Docker daemon`). Rather than being quietly skipped,
they are: named in `IMPLEMENTATION-NOTES.md` under an explicit "Not verified" heading with
the daemon's actual crash message; tracked as open issue `beans-oba` with the exact commands
to run; and covered by a CI job that runs both builds and the `go.work` probe on every PR
touching the app. That is a complete handling of an un-runnable gate.

### The worktree survives being poked at

Every experiment here — `make ci`, `go work sync`, seven `go` bisection commands, ten
script mutants, an 18-shape input probe, a deploy dry-run with a deliberately dirtied tree —
left `git status --porcelain` and `git diff HEAD --stat` empty. The `.gitignore` work on
this branch (anchored `/bn`, `bin/`, `*.test`, `coverage.*`) is why; only `go.work.sum`
escapes it, which is finding I2.
