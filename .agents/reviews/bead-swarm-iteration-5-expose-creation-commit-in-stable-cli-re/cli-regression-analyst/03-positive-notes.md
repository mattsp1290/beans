# Positive Notes

- `cmd/bn/app.go:455` uses `omitempty`, so existing empty creation commit rows retain the old JSON shape.
- `cmd/bn/app.go:502` centralizes the field mapping in `toIssueJSON`, which is the shared serialization helper used by the relevant issue JSON commands.
- `cmd/bn/cmd_show.go:33`, `cmd/bn/cmd_list.go:74`, and `cmd/bn/cmd_ready.go:36` keep JSON output separated from the existing table/detail output paths.
- `cmd/bn/app_test.go:344` exercises the requested command surfaces (`show`, `list`, and `ready`) and verifies both present and omitted `creation_commit` behavior.
