# Critical And Important

VERDICT: REQUEST_CHANGES

## Critical

No Critical issues found.

## Important

### Missing explicit tests for requested best-effort failure classes

- Severity: Important
- File: `cmd/bn/git_resolver_test.go:146`

The bead acceptance criteria call out missing `git` and permission failures as cases that should collapse to `ok=false`. The implementation likely does that because all `exec.Command(...).Output()` errors are collapsed, but the original test matrix only covered outside repo, unborn HEAD, and missing root.

Suggested fix:

```go
t.Run("git not found", func(t *testing.T) {
    t.Setenv("PATH", t.TempDir())
    sha, ok, err := resolver.HeadCommit(t.TempDir())
    if err != nil || ok || sha != "" {
        t.Fatalf("HeadCommit with git missing: got (%q, %v, %v), want empty ok=false nil", sha, ok, err)
    }
})
```

Add a chmod-based Unix permission-denied case as well, with a Windows skip.

