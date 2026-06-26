# Critical And Important

## Critical

No Critical issues found.

## Important

### Important: repo auto-registration happened before import transaction

File: store/store.go:1135

`ImportIssuesFull` prepared repo inputs before starting the import transaction, and `prepareImportRepoInputs` called `AutoRegisterRepo`. That could leave repo/project/audit rows behind when later import validation, conflict handling, or repo link insertion failed.

Suggested fix:

```go
// Validate repo-link fields before auto-registration and keep repo mutation
// behavior aligned with rows that will actually be imported.
if err := validateImportRepoInput(*repoIn); err != nil {
    return nil, fmt.Errorf("store: ImportIssuesFull repo %s: %w", out[i].ID, err)
}
```

### Important: merge imports could not repair existing issue repo links

File: store/store.go:1377

`insertImportIssueRepo` checked only whether a link existed and returned nil. This made merge imports idempotent, but also prevented stale repo routing metadata from being corrected by a later import.

Suggested fix:

```go
if err == nil {
    return replaceImportIssueRepo(ctx, tx, item)
}
```

