# Suggestions

- `cmd/bn/cmd_create_test.go`: The helper `runCreateAndLoadIssue` is useful, but it currently accepts only one optional flag map. If more create tests grow around this path, a typed helper argument could make future cases clearer.
