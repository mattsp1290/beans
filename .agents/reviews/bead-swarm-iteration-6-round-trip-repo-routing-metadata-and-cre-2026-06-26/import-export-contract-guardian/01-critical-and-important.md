# Critical And Important

## Critical

No Critical issues found.

## Important

### Important: merge imports skipped existing repo links

File: store/store.go:1379

When an issue already had a `bn_issue_repos` row, `insertImportIssueRepo` returned immediately. Merge imports therefore could not repair or round-trip repo routing metadata for existing issues.

Suggested fix:

```go
if err == nil {
    return replaceImportIssueRepo(ctx, tx, item)
}
```

### Important: exported clone_strategy was dropped on import

File: cmd/bn/cmd_import.go:47

The export shape included `repo.clone_strategy`, but import did not carry it into store repo registration. A bn export using `fresh-clone` could re-import through `remote_url` and create a registry row with the default clone strategy instead.

Suggested fix:

```go
return &store.ImportRepoInput{
    CloneStrategy: raw.CloneStrategy,
}
```

