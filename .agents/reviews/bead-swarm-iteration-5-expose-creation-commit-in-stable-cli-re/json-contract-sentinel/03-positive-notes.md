# Positive Notes

- `cmd/bn/app.go:455` adds `creation_commit` with `omitempty`, preserving legacy JSON output for repo targets that do not have a captured creation commit.
- `cmd/bn/app.go:502` maps the field from `iss.Repo.CreationCommit`, so all commands that use `toIssueJSON` inherit the same stable shape.
- `cmd/bn/cmd_show.go:33`, `cmd/bn/cmd_list.go:43`, `cmd/bn/cmd_list.go:74`, and `cmd/bn/cmd_ready.go:36` keep JSON output behind the existing `rs.jsonOut` checks; table output remains on the existing renderer paths.
- `cmd/bn/app_test.go:344` covers `show`, `list`, and `ready` JSON output, including both present and omitted `creation_commit` cases.
- `cmd/bn/app_test.go:447` validates field presence separately from value comparison, which directly proves the `omitempty` contract.
