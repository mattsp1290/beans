# Suggestions

## Suggestion 1

- File: `cmd/bn/repo_resolve.go:165`
- Issue: `isFullLowerHexObjectID` duplicates the same lowercase 40-hex object ID validation shape already used by `isFullLowercaseHexCommit` in `cmd/bn/git_resolver.go`. The duplication is not harmful, but future edits could make the two capture guards disagree.
- Suggested change:

```go
// Prefer reusing the existing helper, or move the shared validator to one
// package-local helper with an object-ID-oriented name.
if !isFullLowercaseHexCommit(sha) {
	return ""
}
```

## Suggestion 2

- File: `cmd/bn/repo_resolve_test.go:198`
- Issue: The invalid-output cases cover `"HEAD"` and uppercase hex, which are the important behavioral cases. A whitespace-padded otherwise-valid hash case would pin the exact boundary expected from `cwdCreationCommitForRepo` when a non-real resolver returns untrimmed git output.
- Suggested change:

```go
{
	name:        "whitespace-padded object ID ignored",
	selectedURL: "https://github.com/acme/padded-head",
	gitRoot:     "/home/alice/padded-head",
	gitRemote:   "https://github.com/acme/padded-head",
	gitHead:     " 4567890abcdef1234567890abcdef12345678901\n",
	want:        "",
}
```
