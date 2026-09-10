# Suggestions

None of these blocks anything. They are the residue I found while checking whether
four rounds of fixes left loose ends.

## S1 — The `bn` version string silently degraded to a bare SHA, and only a commit message says so

`libs/beans/Makefile` computes:

```make
VERSION ?= $(shell git describe --tags --match 'libs/beans/v*' --always --dirty 2>/dev/null || echo dev)
```

No `libs/beans/v*` tag exists (the repo has exactly two tags, `v0.1.0` and `v0.1.1`),
so `--always` falls through to the abbreviated SHA. `./libs/beans/bin/bn --version`
now prints `bn version e3875a8`, where before the branch it printed
`v0.1.1-308-ge3875a8`.

The reasoning is sound and `fb0b755` documents it honestly ("No such tag exists yet,
so `--always` falls back to the commit hash; creating the first one is deferred
work"), and `beans-vgn` tracks creating the tag. But note that `14da22e` cited the
old-format output as its evidence that the LDFLAGS path was wired correctly:

> The LDFLAGS version path is correct - `bn --version` prints
> `v0.1.1-298-g6895dea-dirty`.

Two commits later that output shape changed, and nothing in the *tree* records it —
the Makefile comment explains the `--match` convention but not that it currently
matches nothing. A reader running `bn --version` after merge has no in-tree signal
distinguishing "working as designed, pending the first tag" from "the ldflags broke."
One clause in the existing Makefile comment would close it.

## S2 — `IMPLEMENTATION-NOTES.md`'s AC4 exception list is under-inclusive

The notes say:

> **01-target-layout-and-module-graph.md AC4** forbids any path under
> `apps/bean-counter/` beginning `../../..`. The prod compose file's
> `context: ../../..` is correct … and belongs in that criterion's exception list.

Grepping the tree finds three files under `apps/bean-counter/` with `../../..`, not
one:

- `apps/bean-counter/deploy/docker-compose.prod.yml:37` — `context: ../../..` (the
  one named)
- `apps/bean-counter/deploy/README.md:5` — the link to
  `../../../.agents/plans/bean-counter-deploy/`, which resolves correctly and is
  unavoidable given `.agents/` sits at the root
- `apps/bean-counter/test/scripts/deploy-production_test.sh:124,127,131` —
  `../../../fiber-fork`, synthetic hostile fixtures inside test strings, not real
  paths

Both extras are legitimate; neither is listed. In a document whose entire purpose is
"here is where the tree stops matching the plan," an exception list that names one of
three is worth completing. Two lines.

## S3 — `.git/info/exclude` still hides `.agents/reviews/`, so criterion 10 is weaker than it reads

`e3875a8` caught and worked around this:

> a local `.git/info/exclude` entry hides that directory from `git status`, so the
> previous two commits referenced records that were not actually in the repository;
> they are added with `-f` here

The workaround landed (222 files under `.agents/reviews/` are now tracked) but the
exclude entry is still there. Consequences that outlive this branch:

- Plan success criterion 10 (`git status --porcelain` is empty) is partly satisfied
  by concealment on this machine. I cross-checked with `git diff HEAD --stat` and by
  listing tracked review files, and the branch really is clean — but the criterion
  does not prove that by itself here.
- **This pass-4 review directory is invisible to `git status` too.** Whoever commits
  it needs `git add -f`, exactly as `e3875a8` did, or pass 4's records repeat pass 2
  and 3's mistake of being referenced but not committed.

`.git/info/exclude` is not a repository file, so this cannot be fixed in the branch —
it is a one-line local deletion by the operator, and worth a bead so it does not
recur on the next machine that clones this repo.

## S4 — `ci-workspace`'s fan-out check skips the one root target that is not a fan-out

`ci-workspace` loops `make -n` over `build test vet lint tidy-check ci ci-integration
clean` — eight targets, all confirmed resolving. The root Makefile also defines
`fmt-check`, which is deliberately *not* a fan-out (only `apps/bean-counter` has the
target; `libs/beans` enforces gofmt through golangci-lint formatters). Because it is
the odd one out, it is the one most likely to drift, and it is the one the loop
omits.

It is covered in practice — the backend job runs `make fmt-check` from inside
`apps/bean-counter` — so a break surfaces, just via a different workflow than the one
whose stated job is "every target it delegates exists in every module." Adding
`fmt-check` to the loop costs nothing.

## S5 — The two root shell scripts are gated by no workflow

`setup-beads.sh` (19 KB) and `setup-multi-repo-beads.sh` (23 KB) sit at depth 1 and
match no path filter in any of the three workflows, so only the unfiltered
`ci-workspace` runs on a PR that touches them — and `ci-workspace` does not
shellcheck anything. `shellcheck` on them today reports only `SC2016` info-level
notes (`$N` placeholders inside single-quoted `bd create` arguments — correct as
written).

This is not a regression: `main`'s old root `ci.yml` did not shellcheck them either.
But this branch edited both (adding the HISTORICAL headers), and the branch is
simultaneously the one that established shellcheck-in-CI as the convention for
`apps/bean-counter`'s scripts. Either add them to the `deploy-scripts` job, or accept
that one-shot already-executed seeding scripts do not need a gate — both defensible,
worth deciding rather than defaulting.
