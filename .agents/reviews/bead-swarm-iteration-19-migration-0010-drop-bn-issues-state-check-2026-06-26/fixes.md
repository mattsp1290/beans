# Review Fixes

Both independent Codex reviewers returned `REQUEST_CHANGES` with Important test-coverage findings. Their subprocess sandboxes could not write `.agents/reviews`, so these durable artifacts were reconstructed from their final outputs by the orchestrator.

## SQLite Rebuild Guardian

- Finding: `schema/schema_test.go` did not prove live FK behavior, retained non-state constraints/defaults, or cascade behavior after SQLite 0010 rebuilt `bn_issues`.
- Resolution: Added post-v10 assertions for `PRAGMA foreign_key_check`, column type/null/default/PK metadata, missing-parent FK rejection, priority and labels CHECK rejection, and child-row cascade behavior after deleting a migrated issue.
- Status: Fixed and validated.

## Dialect Migration Skeptic

- Finding: `ready_for_*` writes were proven for SQLite but not for Postgres/MySQL after migration 0010.
- Resolution: Added `workflow_hold_states_after_migration` to the integration store contract so Postgres, MySQL, and SQLite each create an issue after store migration and update it through `ready_for_review`, `ready_for_validation`, and `ready_for_merge`.
- Status: Fixed and validated.

## Validation

- `go test ./schema` passed.
- `go test ./store -run 'TestStoreAcceptsNewHoldStates|TestStoreRejectsUnknownStatus|TestStoreCustomWorkflowDefaultAndVocab|TestReadyExcludesHoldAndBlocks'` passed.
- `go test -tags=integration ./schema` passed.
- `go test -tags=integration ./store -run 'TestStoreContractAcrossDialects/(postgres|mysql|sqlite)/workflow_hold_states_after_migration'` passed.
- `make test` passed.

Findings fixed and independently re-reviewed: false. The findings were fixed and validated, but the reviewer subagents were not relaunched after fixes.
