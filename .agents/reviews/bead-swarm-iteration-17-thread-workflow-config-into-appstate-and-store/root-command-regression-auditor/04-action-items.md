Suggested: [cmd/bn/app_test.go:202](/Users/punk1290/git/beans/cmd/bn/app_test.go:202) registers `rs.store.Close` only after `cmd.Execute()` succeeds. If `Execute` opens the store and then fails, the SQLite handle can leak for the rest of the package run. Prefer a cleanup registered before execution that checks `if rs.store != nil { rs.store.Close() }`.

Suggested: [cmd/bn/app_test.go:181](/Users/punk1290/git/beans/cmd/bn/app_test.go:181) uses a fixed shared in-memory SQLite DSN name. It is probably safe here because the test is non-parallel and unique today, but using `t.Name()`/a helper-generated DSN would make accidental future collisions harder.

No blocking issues found. The test correctly avoids `t.Parallel`, isolates cwd/env via `t.Chdir` and `t.Setenv`, uses explicit `BN_CONFIG`, passes `--project` to avoid marker/git prefix dependence, and resets `activeWorkflow`.

VERDICT: APPROVE  
CRITICAL_COUNT: 0  
IMPORTANT_COUNT: 0  
SUGGESTED_COUNT: 2