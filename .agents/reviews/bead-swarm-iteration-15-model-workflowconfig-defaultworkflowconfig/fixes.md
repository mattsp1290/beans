Fixed review findings:

- Reworded the `WorkflowConfig` unknown-status comment so it matches the
  implementation and tests: unknown statuses are invalid, non-active,
  non-terminal, and not hold.
- Added focused `StatusNames` coverage for default vocabulary order and string
  conversion.
- Added `Validate` coverage for invalid transition sources.

Validation after fixes:

- `go test ./model` passed.
- `make test` passed.

Re-review:

- `workflow-contract-auditor`: VERDICT: APPROVE.
- `status-semantics-auditor`: VERDICT: APPROVE.
