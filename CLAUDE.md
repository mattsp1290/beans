# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on
this repository. `AGENTS.md` is the fuller reference; this file records the
build commands, the architecture, and the conventions.

## Beans issue tracker

This repository tracks its own issues with `bn`, the binary it builds. Run
`bn prime` for the rules; the short version:

```bash
bn ready                      # available work
bn show <id>                  # issue detail
bn update <id> --claim        # claim work (status in_progress, assignee you)
bn close <id> -r "reason"     # complete work
bn remember "insight"         # persistent knowledge (project memory)
```

`bn` commits and pushes the hub (`~/.beans/hub`) itself on every mutating
command; never commit hub files by hand. The hub is a separate repository
from this one, so the session-completion checklist below only pushes this
repository's code.

- Use `bn` for ALL task tracking; do NOT use TodoWrite, TaskCreate, or
  markdown TODO lists.
- Use `bn remember` for persistent knowledge; do NOT use MEMORY.md files.

## Session completion

When ending a work session, complete every step. Work is NOT complete until
`git push` succeeds.

1. File issues for remaining work (`bn create`).
2. Run quality gates if code changed (`make ci`).
3. Update issue status (`bn close`, `bn update`).
4. Push this repository:
   ```bash
   git pull --rebase
   git push
   git status   # must show "up to date with origin"
   ```
5. Verify the hub is pushed too: `bn status` shows ahead 0.
6. Hand off: stream next-session context with `bn handoff create --file -`
   and attach it to its governing issue when applicable. Keep `bn note` for
   ordinary issue progress history.

## Build, test and architecture

Use `make ci` for the UI, locked Rust workspace and repository skill checks.
`make build` writes release `bin/bn` and embeds current `ui/dist` without Node.
`make release-build` and `make install` first build the complete UI. Direct
`cargo install --locked --path .` embeds whichever assets are present; run
`make ui-install ui-build` first for the full app. Restore the committed
`ui/dist/index.html` placeholder after validation; never commit built assets.

`AGENTS.md` describes the Rust layout. `src/domain` owns lossless documents;
`src/ops` re-reads files on every replay; `src/gitops/hub.rs` locks, commits hand
edits, fetches/rebases, commits with a Bn-Run nonce and pushes. Three bounded
attempts can replay only owned effects; unowned commits and authored bytes must
survive. `src/server` shares native operations with CLI commands.

`docs/format.md` is normative. Roundtrip fixtures and expected-value cases live
under `tests/fixtures`; real Git/CLI/HTTP journeys use disposable repositories.
System Git must be on PATH. No database or derived persistent index is required.

CI retains the UI job and a Rust job that downloads those assets, builds the
product and runs actual `make verify` on native Linux x86_64. Permanent Verus,
property/model and production coupling controls have separate stated limits in
`docs/verification.md`. Dependency checks use locked Cargo metadata; format and
Clippy cover all workspace targets.
