# Critical And Important Findings

## Important

1. `cmd/bn/workflow.go:80` and `cmd/bn/workflow.go:84` silently accepted unknown TOML/YAML keys. A typo such as `defualt` would inherit defaults instead of failing fast. Suggested fix: use `toml.Decode` with `MetaData.Undecoded()` and a YAML decoder with `KnownFields(true)`.

2. `cmd/bn/workflow.go:69` fell back to defaults for all read failures on discovered config files, not only a file that disappeared after discovery. Suggested fix: return errors for non-`os.IsNotExist` read failures.

