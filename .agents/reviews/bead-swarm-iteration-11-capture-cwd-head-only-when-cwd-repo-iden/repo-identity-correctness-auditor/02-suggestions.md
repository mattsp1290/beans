# Suggestions

- `cmd/bn/cmd_create_test.go`: Consider adding a focused test for the persistent/global `--repo <url>` path (`bn --repo <url> create ...`) in a later coverage bead. The implementation appears to handle it through `resolveRepoContext`, but the new tests mainly cover marker, local create `--repo` slug, auto-detect, and local-only file URL behavior.
