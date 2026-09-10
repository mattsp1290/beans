# Suggestions

### S1 — `bean-counter-log` still instructs bootstrapping the dead remote path

**Where:** Beads issue `bean-counter-log` (open, P1, a dependency of `bean-counter-m0p`).

Its description reads:

> "Clone bean-counter to infra-admin@10.0.0.106:~/git/bean-counter; decide/wire the bn DSN
> secret …"

That path is the one `beans-nlc` exists to replace ("deploy-production.sh now defaults
`--repo-dir` to `$HOME/git/beans` … the remote checkout must be replaced before the next
deploy"), and `beans-nlc` also found the clone and the compose project already live on the
host — so the "clone it" half of `-log` is both stale in its path and already satisfied in
its intent. Round 4 repointed `-m0p`, `-am5` and `-mkg`; `-log` was not in that set. Two
open blockers on the same deploy now describe opposite remote layouts. Either fold `-log`
into `beans-nlc`, or narrow it to the DSN-secret half and repoint the path.

This is a suggestion rather than an Important item because the operator-facing
`deploy/README.md` is unambiguous, and `-m0p` itself names `beans-nlc` as the authority on
the checkout.

---

### S2 — Retract the `go.work.sum` acceptance criteria the implementation did not meet

**Where:** `/Users/punk1290/git/beans/.agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md`
("Stale acceptance criteria"), against
`03-go-module-restructure.md:149` and `:225`, and `00-overview.md:249`.

The notes handle this well for one document — AC2 of `01-target-layout-and-module-graph.md`
is explicitly retracted, with the correct technical reason ("which `go work sync` does not
generate for this workspace", which I confirmed). Three other statements say the opposite
and are not retracted:

- `03-go-module-restructure.md:149` — "Both `go.work` and `go.work.sum` are committed."
- `03-go-module-restructure.md:225` — AC3: "`go.work` and `go.work.sum` are tracked by git."
- `00-overview.md:249` — `go.work.sum` listed as "new, tracked".

Whichever way finding I2 is resolved, one edit to the notes covers all three. This matters
a little more than usual because these plan documents are described as authoritative
(`beans-jt7`: "Plan documents 00-08 are authoritative") and the third app will be added by
following doc 07.

---

### S3 — Give the four redundant containment entries a comment, or a test that names them

**Where:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh`,
`require_repo_root`.

The loop's comment explains why intermediate components are listed ("libs, libs/beans,
libs/beans/schema and libs/beans/schema/migrations can each be a committed symlink out of
the tree"), which is true of the threat but not of the *mechanism* — `require_in_repo`
resolves physically, so the deepest entry already catches an out-of-tree symlink at every
ancestor. That is why four entries are mutation-dead (finding I1). Keeping them is
defensible defense-in-depth; a one-line note saying they are deliberately redundant against
a future change to `require_in_repo`'s resolution strategy would stop the next reader from
concluding, as `faa3de8` did, that each entry is independently load-bearing.

---

### S4 — Two `shellcheck` invocation scopes coexist without a stated policy

**Where:** `/Users/punk1290/git/beans/setup-beads.sh` (4 info findings),
`/Users/punk1290/git/beans/setup-multi-repo-beads.sh` (100 info findings).

`shellcheck` over the deploy pair is clean and that is what CI and the README check.
`shellcheck $(git ls-files '*.sh')` exits 1. I confirmed the counts are byte-identical on
`main` (4 and 100), so this branch introduces nothing — but the monorepo now presents a
single root-level shell surface where there used to be two repos' worth, and "shellcheck is
clean" is stated in `deploy/README.md` without scope. Worth either adding `# shellcheck
disable=SC2086,SC2016` headers to the two setup scripts or saying in one place that the
gate covers the deploy pair only.

---

### S5 — `ci-workspace.yml`'s fan-out loop covers 8 of the root Makefile's 11 targets

**Where:** `/Users/punk1290/git/beans/.github/workflows/ci-workspace.yml`, final step.

The loop is `for t in build test vet lint tidy-check ci ci-integration clean`. `fmt-check`,
`beans` and `bean-counter` are not in it. `fmt-check` is the interesting omission: it is the
one target that deliberately does *not* fan out (it delegates only to `apps/bean-counter`),
so it is exactly the target most likely to break silently if `libs/beans` later grows a
`fmt-check`. `make -n fmt-check` resolves today — I checked all 11 — so this is a coverage
suggestion, not a defect.
