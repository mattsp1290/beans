# Review Fixes

## Critical

- None.

## Important

- `workflow-contract-auditor` at `store/store.go:740`: fixed by making `Store.CloseIssue` choose the first configured terminal workflow state, treat any configured terminal state as idempotently closed, and reject invalid terminal config instead of writing the literal `closed`.

- `cli-regression-sentinel` at `cmd/bn/workflow.go:52`: fixed by adding `Store.WorkflowConfig()` and making `appState.workflowConfig()` fall back to the injected store workflow before the built-in default.

## Validation

- `go test ./cmd/bn ./store ./model` passed after the fixes.

## Re-review

- Not independently re-reviewed after fixes; validation plus this fix artifact documents the resolution.
