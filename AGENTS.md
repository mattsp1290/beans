# Agent Instructions

## Commands

```bash
make build
make test
make vet
make lint
make ci
go test -tags=integration ./...
```

Integration tests use testcontainers and require Docker.

## Non-Interactive Shell Commands

Use non-interactive flags for file operations to avoid hanging on confirmation
prompts:

```bash
cp -f source dest
mv -f source dest
rm -f file
rm -rf directory
cp -rf source dest
```

For remote commands, prefer batch/non-prompting modes such as
`ssh -o BatchMode=yes` and `scp -o BatchMode=yes`.
