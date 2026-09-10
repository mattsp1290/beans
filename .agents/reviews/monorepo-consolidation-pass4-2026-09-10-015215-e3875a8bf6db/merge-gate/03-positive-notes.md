# Positive notes

## The commit messages correct themselves, and that is the reason to trust them

I was asked to hunt for overstatement in the migration's commit messages, on the
grounds that one was already caught. What I found instead was a pattern of unforced
retraction:

- `5bd9e07` withdrew `2ef178e`'s central blocker claim — that
  `go install golangci-lint@v2.1.6` yields a go1.23-built binary that refuses
  `run.go: "1.25"` — after a later reviewer disproved it by execution, and explained
  the actual mechanism (`go install pkg@version` uses max(installed toolchain, module
  go directive) and never downgrades). It kept the v2.12.2 pin but stopped claiming a
  mechanism that does not exist.
- `e3875a8` withdrew `5bd9e07`'s claim that the command-substitution status capture
  in `replace_directives` was load-bearing: "The reviewer could not construct any
  input where it changes the verdict, and neither can I … the claim was stronger than
  the evidence."

Both retractions cost the author something and neither was necessary to ship. That is
the behavior that makes the remaining claims worth checking rather than worth
discounting — and when I checked them, they held. The one number I found wrong (01,
I1) traces to a stale `bd` memory, not to a claim invented at the keyboard.

## The history import is genuinely separable, and I verified it structurally

Rather than trust `bfe40cc`'s message, I checked the shape:

- `git log -1 --format='%H %P' bfe40cc` shows two parents — `d2c6584` (the plan
  commit) and `fba12e9` (the filter-repo'd bean-counter tip). A real merge, not a
  flattened import.
- `git rev-list --count fba12e9` = **114**, matching the claim exactly. And
  `128 − 14 first-parent = 114` reconciles independently.
- Authorship dates survive: the oldest imported commit is `dd88be7 Initial commit`,
  `Sun Jun 14 09:08:24 2026`.
- `git log --oneline -- apps/bean-counter/internal/server/app.go` reaches
  `433d900 ralph: iteration 1 checkpoint - initialize Go Fiber skeleton` **without
  `--follow`**, which is the property `--to-subdirectory-filter` was chosen to buy.
- `git log --follow -- libs/beans/store/store.go` reaches `39d689c Extract bn into
  beans module`, 41 commits deep, so the library side survived the move too.

Plan success criteria 4 and 5 are satisfied in fact, not just in narrative.

## The deferred-work accounting reconciles to the issue

This is the check most likely to quietly not add up, and it adds up exactly:

- `4af7ed0` claims `107 + 54 = 161` issues after the tracker import.
- `40d8e64` claims eight deferred-work issues filed; `2ef178e` adds `beans-8do`. That
  is nine.
- `bd stats` today: **170 total**. `161 + 9 = 170`.

And every one of the six items in plan `08`'s deferred-work table has a corresponding
open bead — `beans-ad3` (archive), `beans-nlc` (remote checkout), `beans-cjy`
(golangci-lint unification, decision D7), `beans-vgn` (first `libs/beans/v*` tag),
`beans-a98` (delete the local clone), `beans-bqw` (composite action) — plus three the
implementation discovered and filed on its own (`beans-nlc` raised to P0,
`beans-ued`, `beans-oba`). Nothing the plan required was dropped instead of tracked,
which was the specific thing I was asked to check.

## `IMPLEMENTATION-NOTES.md` is the right document, and it is mostly right

Every substantive claim in it that I could verify, I did:

- AC2's real depth-1 tracked file list *does* add `.dockerignore` and *does* omit
  `go.work.sum` — I listed it (`git ls-files | awk -F/ 'NF==1'`) and confirmed
  `go work sync` creates no `go.work.sum` for this workspace.
- AC1 holds: no tracked `.go` file outside the two module roots.
- AC3 holds: `mysql/`, `postgres/`, `sqlite/` all present under
  `libs/beans/schema/migrations/`.
- Success criterion 7's "both hold" is right — 170 total ≥ 150, 17 open ≥ 7,
  `bd show bean-counter-m0p` resolves.
- The prod-compose depth correction is right, and non-obvious: that file *is* one
  level deeper, `context: ../../..` *is* correct, and its old `./frontend` really
  would have resolved to `apps/bean-counter/deploy/frontend`, a path that never
  existed. It is now `../frontend`. Catching that by *rendering* the file rather than
  reading it is the right instinct and the notes say so.
- The "Not verified" section is honest about the Docker gap rather than implying
  coverage — including naming the actual Docker Desktop crash. I independently
  confirmed the daemon is down.

Writing down that `bn version` is not a subcommand, that `bd dep tree` needs an id in
this build, and that `bd import --dry-run` does not report creates-vs-updates is the
kind of detail that saves the next agent an hour.

## The CI probe was fixed in the right direction

`5bd9e07` found that `ls /src/go.work` against the *runtime* image could never fail —
alpine restarts from scratch and receives one binary, so `/src` never exists. The
replacement does three things right at once: it targets the named `build` stage
(`--target build`), it asserts a known-present file first
(`test -f /src/libs/beans/go.mod`) so a broken `docker run` cannot read as a pass, and
it comments *why* the runtime image was the wrong place to probe. A check that cannot
fail is worse than no check, and the fix says so out loud.

## Root `make ci` is genuinely hermetic

It runs `go mod tidy` under `GOWORK=off` in both modules and then
`git diff --exit-code go.mod go.sum` — a mutating gate. I ran it and the tree came
back byte-clean by both `git status --porcelain` and `git diff HEAD --stat`. Both
lints reported "0 issues", including `libs/beans`' PATH-invoked `golangci-lint`
(v2.1.6 on this machine) and `apps/bean-counter`'s pinned `go run …@v2.12.2`. The
`GOWORK=off` rationale in both Makefiles is the correct one and matches how the
container actually builds.

## Small things done deliberately rather than by default

- `libs/beans`' `clean` removes an explicit `BIN_DIR` instead of `$(dir $(BIN))`,
  because the latter expands to `./` if `BIN` were ever a bare filename. That is
  `rm -rf ./` avoided by thinking about it.
- `.gitignore` deliberately does *not* adopt bean-counter's `.agents/reviews/` rule,
  with the reason recorded — this repo tracks files there, and the rule would leave
  them permanently modified-but-ignored.
- `.dockerignore` avoids a blanket `frontend` exclusion, with the reason recorded:
  the UI image's context *is* the frontend directory, and a root-level exclusion
  breaks the moment a second app ships a UI.
- `fmt-check` driven by `git ls-files '*.go'` with an explicit refuse-on-empty, after
  the `find .`-into-`node_modules` bug *and* the fail-open-on-missing-directory bug
  were both found. Two rounds, two real defects, one clean end state.
- 103 relative markdown links across the tree, zero broken. No `TODO`/`FIXME`/`XXX`
  introduced anywhere in the 21,536 added lines.
