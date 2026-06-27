Review fixes:

- root-command-regression-auditor suggested registering Store cleanup before command execution; fixed by adding a nil-checked `t.Cleanup` before `cmd.Execute`.
- root-command-regression-auditor suggested avoiding a fixed shared SQLite memory DSN; fixed by deriving the DSN name from `t.Name()`.

Revalidation after fixes:

- `go test ./cmd/bn -run 'Test(RootCreateUsesConfiguredWorkflowDefaultStatus|Workflow|StoreConfigFromEnv)' -count=1` passed.
- `go test ./store -run 'TestStore.*Workflow|TestStoreCustomWorkflowDefaultAndVocab' -count=1` passed.
- `make test` passed.
