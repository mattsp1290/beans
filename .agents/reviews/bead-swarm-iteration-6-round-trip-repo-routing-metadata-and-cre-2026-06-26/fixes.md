# Review Fixes

Validation after fixes:

- `go test ./cmd/bn ./store`: pass
- `make test`: pass

Resolved items:

- Import Export Contract Guardian Important item for existing repo links: fixed by replacing existing import repo links and preserving the existing creation_commit when the import does not provide one.
- Import Export Contract Guardian Important item for clone_strategy: fixed by carrying `repo.clone_strategy` through import and `AutoRegisterRepo`.
- Store Transaction Skeptic Important item for repo auto-registration side effects: fixed by moving import remote-url registration into the import transaction, validating repo-link fields before registration, and skipping create-only duplicate or invalid-state rows before registering repos.
- Store Transaction Skeptic Important item for existing repo links: fixed by replacing existing repo links on merge import.

Findings fixed re-reviewed by independent subagents: false.
