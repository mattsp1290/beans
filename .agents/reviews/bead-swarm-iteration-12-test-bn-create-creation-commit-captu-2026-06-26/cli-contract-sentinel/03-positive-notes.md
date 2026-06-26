# Positive Notes

- `cmd/bn/cmd_create.go:125` fixes the JSON output contract by writing through `cmd.OutOrStdout()`, which makes command tests and embedded command execution reliable instead of leaking JSON to process stdout.

- `cmd/bn/cmd_create_test.go:307` covers the create path end to end enough to catch both store persistence and JSON projection of `repo.creation_commit`.

- `cmd/bn/cmd_create_test.go:344` explicitly checks omitted `creation_commit` behavior for identity mismatch and invalid git output. That is the right shape for a field tagged `omitempty` in `cmd/bn/app.go:455`.

- `cmd/bn/cmd_create_test.go:399` verifies that when the prefix guard rejects a foreign auto-detected repo, the entire `repo` object is omitted from create JSON rather than emitting a partial or misleading repo payload.

- `cmd/bn/repo_resolve.go:155` reads HEAD only after the cwd repo identity has been matched to the selected repo. That keeps create behavior conservative and avoids capturing a commit from an unrelated checkout.

- `cmd/bn/repo_resolve.go:159` rejects invalid or symbolic HEAD strings before calling the store. This preserves create's best-effort behavior instead of turning malformed git output into a user-visible create failure.

