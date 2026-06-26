# Positive Notes

- `cmd/bn/app_test.go`: The CLI JSON test exercises all three requested commands, including the added empty-value `show` case.
- `cmd/bn/cmd_export_test.go`: The export test uses the command path and decodes the emitted JSONL, which would catch regressions in command wiring as well as helper serialization.
- `cmd/bn/cmd_import_test.go`: The added slug import test complements the existing remote URL auto-registration and unresolved slug failure tests.
