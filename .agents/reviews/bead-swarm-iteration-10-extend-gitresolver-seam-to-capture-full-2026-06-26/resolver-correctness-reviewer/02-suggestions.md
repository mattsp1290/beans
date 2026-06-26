# Suggestions

## Suggestions

- `cmd/bn/git_resolver_test.go:126`: Tighten the rebase-state assertion so it proves the expected active HEAD exactly, instead of only proving it differs from the topic/base commits.

  Suggested shape:

  ```go
  if want != main {
      t.Fatalf("rebase state HEAD = %s, want main commit %s", want, main)
  }
  ```

- `cmd/bn/git_resolver_test.go:146`: Add an explicit missing-`git` case using a temporary empty `PATH`; this documents the git-not-found collapse path directly.

  Suggested shape:

  ```go
  t.Setenv("PATH", t.TempDir())
  sha, ok, err := resolver.HeadCommit(t.TempDir())
  ```

