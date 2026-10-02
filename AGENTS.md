# Agent Instructions

This repository is a Rust workspace that builds `bn`, a git-backed issue tracker
and wiki for humans and coding agents. Markdown documents live in the separate
hub cloned at `~/.beans/hub`; `bn serve` provides its board and wiki.

## Repository layout

| Path | Purpose |
| --- | --- |
| `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml` | Locked workspace and pinned compiler |
| `src/cli/` | Command parsing and native command journeys |
| `src/domain/` | Lossless frontmatter, note schemas, configuration and plans |
| `src/vault/` | Hub resolution, nofollow index reads, queries and watcher |
| `src/gitops/` | Locks, journal, Git replay and tree recovery |
| `src/ops/` | Replay-safe mutations shared by CLI and HTTP |
| `src/markdown/`, `src/server/` | Comrak rendering and Axum HTTP/SSE |
| `crates/beans-kernel/` | Same-source verified retry and splice bodies |
| `tests/`, `tools/verification/` | Native regressions, properties, models and proof controls |
| `ui/`, `build.rs` | Svelte app and compile-time embedded assets |
| `examples/agent_contract.rs` | Native YAML/workflow interface for repository skills |
| `docs/`, `.agents/skills/` | Product format and agent workflows |

## Commands

```bash
make ci               # UI checks/build, locked Rust checks/tests/build and skill tests
make build            # release bin/bn; embeds current ui/dist, no Node needed
make test             # locked workspace tests
make ui-build         # compile Svelte app into ui/dist
make release-build    # complete UI and release binary
make install          # complete UI and locked Cargo installation
make verify           # native tests, Verus proof and production coupling controls
```

`ui/dist/index.html` is the committed placeholder. Restore it after building;
never commit generated UI assets. Native proof requires Linux x86_64, with
pinned rustc-dev and llvm-tools components; aarch64 compilation is not proof.

## Issue tracking

This repository's issues live in the hub under `projects/beans/`. Run
`bn prime` for the rules; `bn ready`, `bn show <id>`, `bn update <id>
--claim`, `bn close <id> -r "reason"` are the daily loop. `bn` commits and
pushes the hub itself; never commit hub files by hand. `CLAUDE.md` has the
session-completion checklist. Create continuation context with `bn handoff
create --file - [--issue <id>]`; discover it with `bn handoff list`, not
`bn ready`.

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on
confirmation prompts.

Shell commands like `cp`, `mv`, and `rm` may be aliased to include `-i`
(interactive) mode on some systems, causing the agent to hang indefinitely
waiting for y/n input.

**Use these forms instead:**

```bash
# Force overwrite without prompting
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file

# For recursive operations
rm -rf directory            # NOT: rm -r directory
cp -rf source dest          # NOT: cp -r source dest
```

**Other commands that may prompt:**

- `scp` - use `-o BatchMode=yes` for non-interactive
- `ssh` - use `-o BatchMode=yes` to fail instead of prompting
- `apt-get` - use `-y` flag
- `brew` - use `HOMEBREW_NO_AUTO_UPDATE=1` env var

## Workflow configuration

Issue statuses come from `domain::workflow::WorkflowConfig`, loaded with the precedence
`BN_CONFIG` > project `beans.toml` `[workflow]` > hub `beans.toml`
`[workflow]` > built-in defaults, merged per key. Defaults include
`ready_for_review`, `ready_for_validation`, and `ready_for_merge` as hold
states: valid statuses that `bn ready` never returns and that do not satisfy
blockers. See `docs/beans.toml.example`.

## Versioning

`Makefile` derives `VERSION` from `git describe --tags --match 'v*' --always
--dirty`, falling back to `dev` outside Git. `VERSION=...` overrides make builds;
direct Cargo builds accept `BN_VERSION=...`. `build.rs` embeds the version and
UI bytes. `bn --version` prints it. See `docs/release.md`.
