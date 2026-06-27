# Critical And Important Findings

## Important

### Direct command paths can still validate/filter against the default workflow

- Severity: Important
- File path and line number: `cmd/bn/workflow.go:52`
- Description: `workflowConfig()` falls back to `model.DefaultWorkflowConfig()` whenever `appState.workflow` is empty. That is fine for pure helpers, but several existing tests and embeddable command paths construct commands directly with an already-open `rs.store` and do not run the root `PersistentPreRunE`. With a store opened under a custom workflow, `newUpdateCmd` will reject configured statuses such as `qa`, `newReadyCmd` will pass default active/terminal sets to `ReadyIssues`, and `newListCmd` will warn using the default vocabulary. The new root-command test covers the normal CLI path, but the direct-command paths called out in the review scope remain out of sync with the store's actual workflow.
- Suggested fix snippet:

```go
// store/store.go
func (s *Store) WorkflowConfig() model.WorkflowConfig {
	if s == nil || len(s.workflow.Statuses) == 0 {
		return model.DefaultWorkflowConfig()
	}
	return s.workflow
}

// cmd/bn/workflow.go
func (rs *appState) workflowConfig() model.WorkflowConfig {
	if len(rs.workflow.Statuses) != 0 {
		return rs.workflow
	}
	if rs.store != nil {
		return rs.store.WorkflowConfig()
	}
	return model.DefaultWorkflowConfig()
}
```

Add a direct-command regression test with a custom-workflow store and an unset `rs.workflow`, for example `newUpdateCmd` accepting `--status qa` and `newReadyCmd` returning issues whose state is in the custom `Active` bucket.
