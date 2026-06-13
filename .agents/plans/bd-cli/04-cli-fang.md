# 04 — CLI implementation (fang + Cobra)

## Framework

`bn` is a standard **Cobra** command tree executed through **fang**
(`charmbracelet/fang`). fang wraps a `*cobra.Command` root and adds styled help/
usage, styled silent errors (no usage dump on error), `--version` from build
info, a hidden `man` command (manpages via mango), and a `completion` command —
the developer still defines the normal Cobra tree; fang enhances at run time.

```go
// cmd/bn/main.go
func main() {
    root := newRootCmd()           // builds the cobra tree below
    if err := fang.Execute(
        context.Background(), root,
        fang.WithVersion(buildmeta.Version),   // verify exact option names at impl
        // fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM), // graceful ctx
    ); err != nil {
        os.Exit(1)
    }
}
```

> Note: the fang README documents `fang.Execute(ctx, cmd)` and "customizable color
> schemes"; the functional options (`WithVersion`, `WithNotifySignal`,
> `WithoutCompletions`, `WithoutManpage`, `WithColorSchemeFunc`, …) exist in the
> package but aren't all in the README — **confirm exact names/signatures against
> the pinned fang version at implementation time** (grounding gate).

## Command tree

```
bn
├── init           --prefix --non-interactive --quiet
├── create <title> -d -p -l(repeat) -t --silent --actor
├── ready          --json -n
├── list           --status --all -n --json
├── show <id>      --json
├── update <id>    --claim --status --title --description --notes --append-notes
├── close <id>     -r/--reason --force
├── delete <id>    --force
├── dep
│   ├── add <child> <parent>
│   ├── remove <child> <parent>
│   ├── tree        --json
│   └── cycles
├── remember <body> --type --tag        (05)
├── memories <kw>   --json               (05)
├── export                               (06)
├── import [file]                        (06)
└── prime                                (static workflow help)
```

Each command's `RunE` parses flags → calls the `store` package (02) → formats
output. Keep command files thin; all logic in `store`.

## Global / persistent flags

- `--json` (persistent) — toggles machine output where a command supports it.
- `--actor <name>` (persistent) — audit actor; default `$BN_ACTOR`/git/`$USER`.
- `--project`/`--prefix` (persistent, optional) — scope multi-project DBs;
  default `$BN_PROJECT` or the single registered project.
- Connection from `$BN_DSN` (env; not a flag, to avoid DSN-on-argv leakage).

## Output contracts (must match bd — see 01)

- `create --silent` → **bare id + newline** on stdout, nothing else. Implement as
  a hard rule: in silent mode, write only `fmt.Println(id)`; all diagnostics to
  stderr; no fang styling on that stdout line (styling on stdout would corrupt
  `ID=$(bn create … --silent)`). **Guard with a test.**
- `--json` → stable JSON (issue objects keyed like bd's: `id,title,status,…`).
  fang styles help/errors, not your stdout data — keep JSON/`--silent` writes raw.
- Errors → stderr, classifiable text (not-found/validation/conflict), non-zero
  exit. fang's "silent errors" (no usage dump) is the right default for scripted
  use.

## Build / distribution

- `cmd/bn` builds in the beans module (02); `go build ./cmd/bn` →
  `bn` binary. Version via `buildmeta` (existing pattern).
- fang gives `bn completion <shell>` and `bn man` for free; `bn --version` from
  the injected version.
- Document install (`go install …/cmd/bn@…` or a Makefile target) and that `bn`
  must be on PATH for the updated skills (07).

## Risks specific to the CLI layer

- **fang styling vs machine output:** ANSI/styled writes must never touch the
  `--silent`/`--json` stdout path (only help/errors). The test in 08 pins this.
- **Option-name drift:** the fang option names above are provisional — confirm
  against the pinned version before writing `main.go` (grounding gate, like the
  postgres-tracker V1/V2 gate).
- **TTY vs non-TTY:** agents invoke `bn` non-interactively; ensure no interactive
  prompts (mirror bd's `--non-interactive`); fang/Cobra must not block on a TTY.
