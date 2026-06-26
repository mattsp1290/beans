# Positive Notes

- `cmd/bn/git_resolver_test.go:82`: The success-state coverage uses real git repositories, which is appropriate for testing the production resolver seam.
- `cmd/bn/git_resolver_test.go:108`: Merge-conflict state is exercised directly and verifies the exact current `HEAD`.
- `cmd/bn/git_resolver_test.go:126`: Rebase-conflict state is represented with a real conflicting rebase rather than a synthetic directory layout.
- `cmd/bn/git_resolver_test.go:101`: The submodule case uses a real nested repository and validates the submodule HEAD, covering an important repo-boundary edge case.

