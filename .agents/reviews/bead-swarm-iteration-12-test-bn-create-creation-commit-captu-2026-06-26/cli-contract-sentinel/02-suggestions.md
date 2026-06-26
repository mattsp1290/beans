# Suggestions

## Suggestion: Strengthen the create `--json` stdout contract assertion

- File: `cmd/bn/cmd_create_test.go:543`
- Severity: Suggestion

`runCreateJSONAndLoadIssue` now captures `cmd.SetOut(&buf)`, which is the right regression shape for the `cmd_create.go:125` fix. The helper proves the JSON is parseable and checks `creation_commit` presence, but it does not explicitly assert the command-output contract that scripts depend on: one complete JSON document on the command output stream, terminated by exactly one newline, with no leading/trailing non-JSON bytes. A future change could accidentally prepend status text to the captured writer and still produce a harder-to-diagnose JSON parse failure rather than a direct contract failure.

Suggested test tightening:

```go
raw := buf.Bytes()
if !bytes.HasSuffix(raw, []byte("\n")) || bytes.Count(raw, []byte("\n")) == 0 {
	t.Fatalf("create --json output should be newline-terminated JSON, got %q", raw)
}
if bytes.TrimSpace(raw)[0] != '{' {
	t.Fatalf("create --json output has non-JSON prefix: %q", raw)
}
```

This is not blocking because the current tests already catch the main regression: `writeJSONTo(cmd.OutOrStdout(), ...)` is exercised through `cmd.SetOut(&buf)`, and the omitted-field behavior is covered by decoding into `map[string]any`.

