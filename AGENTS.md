# Agent Instructions

## Commands

```bash
make build
make test
make vet
make lint
make ci
go test -tags=integration ./...
```

Integration tests use testcontainers and require Docker.

## Workflow Configuration

Issue statuses are driven by `model.WorkflowConfig` and can be configured with
`BN_CONFIG`, `bn.toml`, `bn.yaml`, or `$XDG_CONFIG_HOME/bn/config.*`. Defaults
include `ready_for_review`, `ready_for_validation`, and `ready_for_merge` as
hold states: they are valid statuses but are not returned by `bn ready` and do
not satisfy blockers. Keep CLI help, table output, import/update validation,
and docs aligned with the configured workflow vocabulary. See
`docs/bn.toml.example` for the operator-facing config template.

## Non-Interactive Shell Commands

Use non-interactive flags for file operations to avoid hanging on confirmation
prompts:

```bash
cp -f source dest
mv -f source dest
rm -f file
rm -rf directory
cp -rf source dest
```

For remote commands, prefer batch/non-prompting modes such as
`ssh -o BatchMode=yes` and `scp -o BatchMode=yes`.

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
