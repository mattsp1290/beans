# Agent Instructions

This repository is a monorepo. Two Go modules live here today, and a third
application will join later.

## Repository layout

```text
beans/
├── go.work                       tracked workspace: ./libs/beans, ./apps/bean-counter
├── Makefile                      fan-out only; each module owns its build rules
├── .dockerignore                 for builds whose context is the repository root
├── .beads/                       the single issue tracker for the whole repo
├── .agents/plans/                plans and dated records, namespaced by project
├── libs/
│   └── beans/                    module github.com/mattsp1290/beans/libs/beans
│                                 the beans library and the `bn` CLI
└── apps/
    └── bean-counter/             module github.com/mattsp1290/beans/apps/bean-counter
                                  Go + Fiber API and Svelte UI over the beans store
```

Where work belongs:

- Library or `bn` CLI change -> `libs/beans/`.
- bean-counter API or UI change -> `apps/bean-counter/`.
- Shared code a second application would also need -> a new `libs/<name>/`
  module, never an application's `internal/` (Go forbids importing that across
  module boundaries, and the compiler error arrives late).
- A new application -> `apps/<name>/`. Follow
  `.agents/plans/monorepo-consolidation/07-third-app-slot.md`, which is the
  checklist for wiring one in.

Each application depends on the library through a filesystem `replace`, not a
published version:

```text
require github.com/mattsp1290/beans/libs/beans v0.0.0
replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
```

That `replace` is mandatory. The container build and every `GOWORK=off`
invocation resolve through it; `go.work` is a convenience for editors and
cross-module work, not the mechanism. `apps/bean-counter`'s deploy script
refuses to deploy if the replace is missing or points anywhere else.

## Commands

From the repository root:

```bash
make ci               # vet, lint, test, build, tidy-check across every module
make ci-integration   # testcontainers suites; requires a running Docker daemon
make build            # every module
make test             # every module
make fmt-check        # apps/bean-counter (libs/beans enforces gofmt via golangci-lint)

make beans TARGET=build          # run one target in libs/beans
make bean-counter TARGET=test    # run one target in apps/bean-counter
```

Inside a module, its own Makefile is authoritative. Each module keeps its own
`.golangci.yml` and its own golangci-lint version; do not unify them without
deciding to.

Integration tests use testcontainers and require Docker. `make ci` deliberately
excludes them so the full non-integration gate runs without a daemon.

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

## libs/beans

The beans library and the `bn` CLI. No intra-repo dependencies.

### Workflow Configuration

Issue statuses are driven by `model.WorkflowConfig` and can be configured with
`BN_CONFIG`, `bn.toml`, `bn.yaml`, or `$XDG_CONFIG_HOME/bn/config.*`. Defaults
include `ready_for_review`, `ready_for_validation`, and `ready_for_merge` as
hold states: they are valid statuses but are not returned by `bn ready` and do
not satisfy blockers. Keep CLI help, table output, import/update validation,
and docs aligned with the configured workflow vocabulary. See
`libs/beans/docs/bn.toml.example` for the operator-facing config template.

### Versioning

`libs/beans/Makefile` derives `VERSION` from `git describe --tags --match
'libs/beans/v*'`, Go's convention for a module in a subdirectory, so an
application's tag can never be read as a library version. No such tag exists
yet, so `--always` falls back to a commit hash. The pre-monorepo `v0.1.0` and
`v0.1.1` tags remain as historical markers and no longer resolve to a module.

`bn --version` prints the linked version. The `LDFLAGS` path in that Makefile
must match the module path exactly: a wrong path produces an empty version
string with no build error.

## apps/bean-counter

Go + Fiber v3 API and a Svelte UI over the beans store, for trusted local or
private networks. No authentication by design; mutations are attributed to the
configured `BN_ACTOR`.

- `internal/store` is the only boundary around beans store types, re-exporting
  them as package-level aliases. Keep beans types out of the HTTP layer.
- The beans store owns schema migration. Never add bean-counter migrations for
  beans-owned tables.
- Its images build from the **repository root**, not from the application
  directory, because the build needs `libs/beans`:

  ```bash
  docker build -f apps/bean-counter/Dockerfile -t bean-counter-api .
  docker build -t bean-counter-ui ./apps/bean-counter/frontend
  ```

- `scripts/deploy-production.sh` runs from the repository root and its
  clean-worktree gate covers the whole monorepo. See
  `apps/bean-counter/deploy/README.md`.
- Longer-form decisions live in `apps/bean-counter/prompts/docs/architecture-decisions.md`.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
