# Suggestions

- `cmd/bn/cmd_create_test.go`: Add focused coverage for the persistent/global `--repo <url>` create path (`bn --repo <url> create ...`) to prove URL-selected repos also capture HEAD only on cwd identity match.
- `cmd/bn/cmd_create_test.go`: Add a local-only `file://` marker or explicit repo case, not only local-only auto-detect, so the synthesized file URL comparison is covered for every selected-repo source.
- `cmd/bn/cmd_create_test.go`: The helper `runCreateAndLoadIssue` is useful, but it currently accepts only one optional flag map. If more create tests grow around this path, a typed helper argument could make future cases clearer.
