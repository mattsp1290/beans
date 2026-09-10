# Suggestions

### S1 — `apps/bean-counter/internal/config/config.go:107` names a pre-monorepo plan path

```go
// .agents/plans/deploy/. Exactly one source may be set; setting both is an
```

Same stale path as I1, in a code comment rather than a link. Should be
`.agents/plans/bean-counter-deploy/`. Together with I1 these are the only two surviving
references to the old plan directory anywhere outside `.agents/` (executed: `git grep -n -E
'\.agents/plans/(deploy|beans-0-1-1)' -- . ':!.agents'`).

Worth noting how clean the rest of the sweep was: **zero** occurrences of the old module
path `github.com/mattsp1290/beans/{model,repo,store,schema,version}`, **zero** of
`github.com/mattsp1290/bean-counter`, and **zero** of `$HOME/git/bean-counter` outside
`.agents/`.

### S2 — `apps/bean-counter/Makefile` `fmt-check` traded a coverage cliff for the node_modules fix

`2ef178e` changed:

```make
-	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))" || (...)
+	@test -z "$$(gofmt -l ./cmd ./internal ./test)" || (...)
```

The motivation was sound — the bare `find .` descended into `frontend/node_modules` and the
`./.git/*` exclusion went inert once this stopped being a repository root. But the
replacement has two sharp edges:

1. It silently stops covering any new top-level Go directory. Today there is nothing to
   miss (executed: no tracked `.go` under `apps/bean-counter/` outside `cmd/`, `internal/`,
   `test/`), so this is latent, not live. But `07-third-app-slot.md` invites new modules to
   copy this Makefile, and the failure mode is a green gate rather than an error.
2. If one of the three directories is ever removed or renamed, `gofmt -l` writes its error
   to stderr while `$( )` captures only stdout, so `test -z` sees an empty string and the
   check passes. A formatting gate that goes green when its input vanishes is the worst
   shape for this kind of check.

`gofmt -l $$(git ls-files '*.go')` gets the node_modules fix (untracked files are excluded
by construction), keeps full coverage, and fails loudly on a bad path.

### S3 — Root `Makefile` `fmt-check` is the one target that does not fan out

Every other root target loops over `$(MODULES)`; `fmt-check` hardcodes
`$(MAKE) -C apps/bean-counter fmt-check`. That is correct today and the comment explains
why (`libs/beans` enforces gofmt through golangci-lint formatters instead — verified, see
`03-positive-notes.md`). But it also means `fmt-check` is excluded from root `ci` *and*
from `ci-workspace`'s "Root Makefile fans out to every module" gate list, so when the third
application lands it will be silently skipped by both.

Either fan out with a per-module opt-out, or leave a note at the `MODULES` line pointing at
`07-third-app-slot.md` so whoever adds the third module knows this target needs a decision.

### S4 — Workflow path filters list `go.work.sum`, which does not and will not exist

All three workflows filter on `'go.work.sum'`. Executed: the file is untracked, has never
been tracked (`git log --all -- go.work.sum` is empty), and `go work sync` at the root does
not create one. The filter entry is inert. Harmless future-proofing, but a reader
reasonably infers the file exists — a trailing comment or removal would help.

### S5 — `ci-libs-beans.yml` runs integration tests with no Docker preflight

Its final step is `go test -tags=integration ./...`, and `libs/beans`' Makefile comment says
that suite requires Docker (testcontainers). The `ci-apps-bean-counter` integration job
guards this with a `docker version` step; this one does not. On a runner without a daemon
the failure surfaces as an opaque testcontainers timeout rather than a one-line "no Docker".
Adding the same `- name: Check Docker / run: docker version` step makes the two jobs
symmetric.

### S6 — Imported Beads issue descriptions still name pre-monorepo paths

`bd show bean-counter-m0p` returns:

```
Run scripts/deploy-production.sh --ref main; verify per .agents/plans/deploy/05-validation.md; ...
```

Both paths are dead. This is *per plan* — `06-beads-and-agent-config.md` exclusions say
"No issue is closed, reopened, retitled, or reprioritized by this work package. The import
is a move, not a triage pass" — so it is not a violation. But `bean-counter-m0p` is the
first live production deploy, it is `P0`, and the operator who opens it before that deploy
gets two paths that do not resolve. A small follow-up issue to re-path the imported
descriptions would close the loop without breaking the no-triage rule for this branch.

### S7 — `ci-workspace.yml`'s header comment is now half-true

```yaml
# Unfiltered on purpose: this is the one job that always runs, so a change
# anywhere still proves the workspace itself is coherent.
on:
  push:
    branches:
      - main
  pull_request:
```

`2ef178e` added `push.branches: [main]`. The workflow is still
*path*-unfiltered and still runs on every pull request, so the property `CLAUDE.md` depends
on — "require `ci-workspace` and only `ci-workspace`" as a status check — is intact. The
word "unfiltered" just no longer describes the trigger block sitting under it. One word
("path-unfiltered") fixes it.

### S8 — New review records are hidden from `git status` by a repo-local exclude

**Executed.** After writing this review's five files, `git status --porcelain` was still
empty. The cause is `.git/info/exclude:7`, which contains `.agents/reviews/`:

```
$ git check-ignore -v .agents/reviews/.../regression-auditor/00-overview.md
.git/info/exclude:7:.agents/reviews/	.agents/reviews/.../00-overview.md
```

`.git/info/exclude` is machine-local and untracked, so this is not a branch defect. But it
has two consequences worth knowing:

1. It partially contradicts `01-target-layout-and-module-graph.md`, which resolved the
   `.agents/reviews/` collision by having the root `.gitignore` *not* ignore the directory
   "matching the beans behavior, so existing tracked reviews stay tracked". Already-tracked
   reviews do stay tracked, so the stated goal holds — but new ones are invisible on this
   machine and need `git add -f`. The three review passes on this branch are exactly the
   records the plan wanted preserved.
2. It slightly weakens success criterion 10 as a signal. An empty `git status --porcelain`
   no longer proves nothing was written under `.agents/reviews/`. Every other gate I ran
   was still verified against a genuinely clean tree, so the criterion holds on its own
   terms — just note that this one directory is outside its reach.

Either `git add -f` this pass's records, or decide deliberately that review output is
local-only and say so in `06-beads-and-agent-config.md`.
