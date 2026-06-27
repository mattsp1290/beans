# Critical And Important Findings

## Important

Severity: Important

File path and line number: `store/store.go:740`

Description: `CloseIssue` still writes the literal `"closed"` directly to `bn_issues.state` and considers only `"closed"` idempotent. This bypasses the store's `WorkflowConfig` validation used by `UpdateIssue` and import writes. With a valid custom workflow such as `statuses = ["open", "shipped"]` and `terminal = ["shipped"]`, `bn close` can persist `"closed"`, which is outside the configured vocabulary. That violates the write-strict half of the read-tolerant/write-strict invariant.

Suggested fix snippet:

```go
terminalStates := s.workflow.Terminal
if len(terminalStates) == 0 {
	return fmt.Errorf("%w: no terminal status configured", ErrInvalidIssueState)
}
closeState := terminalStates[0]

terminalStrings := make([]string, len(terminalStates))
for i, st := range terminalStates {
	if !s.workflow.IsValid(st) {
		return fmt.Errorf("%w: %s", ErrInvalidIssueState, st)
	}
	terminalStrings[i] = string(st)
}

res := db.WithContext(ctx).
	Model(&gormIssue{}).
	Where("id = ? AND state NOT IN ?", id, terminalStrings).
	Updates(map[string]any{"state": string(closeState), "updated_at": newGORMTime(clockNowUTC())})
```

Then add a store or CLI test that creates a custom workflow with terminal `shipped`, calls close, and asserts the persisted state is `shipped` and repeated close remains idempotent.
