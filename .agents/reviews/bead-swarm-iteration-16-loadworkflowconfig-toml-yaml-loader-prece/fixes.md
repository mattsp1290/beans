# Review Fixes

Fixed the two Important findings from Config Validation Auditor.

- `cmd/bn/workflow.go`: TOML decoding now uses `toml.Decode` and fails when `MetaData.Undecoded()` reports unknown keys. YAML decoding now uses `yaml.Decoder` with `KnownFields(true)`.
- `cmd/bn/workflow.go`: discovered config read errors now fall back only for `os.IsNotExist`; all other read errors fail fast.
- `cmd/bn/workflow_test.go`: added regression coverage for unknown TOML/YAML keys and discovered unreadable config files.

Validation after fixes:

- `go test ./cmd/bn ./model` passed.

Findings fixed re-reviewed: false. The fixes were validated locally and recorded here, but a second independent review pass was not run after the fixes.
