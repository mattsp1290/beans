# 02 — Config System

## What exists today

Configuration is **env-only** and hand-parsed in `cmd/bn/app.go`:

- `storeConfigFromEnv()` (`cmd/bn/app.go:144`) reads `BN_DSN`, `BN_DRIVER`.
- `BN_PROJECT`, `BN_ACTOR` are read elsewhere in `app.go`.
- A `.bn` marker file holds `project=`/`repo=`/`remote=` (`cmd/bn/app.go:265`)
  in a bespoke `key=value` format — **not** TOML/YAML.

There is **no** config-file library in `go.mod` and no TOML/YAML parsing today.
This is greenfield.

## The `WorkflowConfig` type

A single struct becomes the runtime source of truth for status decisions. It
belongs in the `model` package (the model already references the concept by name
at `model/issue.go:46`), so both `store` and `cmd/bn` can depend on it without a
cycle.

```go
// model/workflow.go (new)
package model

type WorkflowConfig struct {
    Statuses []IssueState            // ordered vocabulary; order drives display
    Default  IssueState              // status assigned to new issues
    Active   []IssueState            // dispatchable (bn ready)
    Terminal []IssueState            // "done" for blocker + cleanup semantics
    // Transitions is reserved for a later phase. nil/empty => unrestricted.
    Transitions map[IssueState][]IssueState
}
```

Plus helper methods (all O(1)/O(n) over a tiny slice, no allocation in hot path
if backed by a precomputed set):

```go
func (w WorkflowConfig) IsValid(s IssueState) bool   // s ∈ Statuses
func (w WorkflowConfig) IsActive(s IssueState) bool   // s ∈ Active
func (w WorkflowConfig) IsTerminal(s IssueState) bool // s ∈ Terminal
func (w WorkflowConfig) IsHold(s IssueState) bool     // valid && !active && !terminal
```

### Built-in default

`model.DefaultWorkflowConfig()` returns the vocabulary from
`01-status-model.md`. This is what loads when no config file is found, and it is
a **superset** of today's behavior plus the three new statuses — so an operator
who upgrades and does nothing gets the new statuses and identical
ready/terminal semantics.

## Config file format

Format is chosen by file extension. Both are first-class.

### TOML (`bn.toml`)

```toml
[workflow]
# Ordered status vocabulary. Drives validation and display order.
statuses = [
  "open",
  "in_progress",
  "ready_for_review",
  "ready_for_validation",
  "ready_for_merge",
  "blocked",
  "closed",
  "done",
]

# Status assigned to newly created issues.
default = "open"

# Dispatchable states (bn ready).
active = ["open"]

# Terminal states (satisfy blockers, trigger workspace cleanup).
terminal = ["closed", "done"]

# Optional, reserved for a future phase. Omit for unrestricted transitions.
# [workflow.transitions]
# in_progress          = ["ready_for_review"]
# ready_for_review     = ["ready_for_validation", "in_progress"]
# ready_for_validation = ["ready_for_merge", "ready_for_review"]
# ready_for_merge      = ["closed"]
```

### YAML (`bn.yaml`)

```yaml
workflow:
  statuses:
    - open
    - in_progress
    - ready_for_review
    - ready_for_validation
    - ready_for_merge
    - blocked
    - closed
    - done
  default: open
  active: [open]
  terminal: [closed, done]
  # transitions:            # optional, future phase
  #   in_progress: [ready_for_review]
```

## Load precedence

Lowest to highest priority (later overrides earlier):

1. **Built-in defaults** — `model.DefaultWorkflowConfig()`.
2. **Config file**, if found. Resolution order for the path:
   a. `BN_CONFIG` env var (explicit path; error if set but unreadable).
   b. `./bn.toml`, then `./bn.yaml`/`./bn.yml` in the working directory.
   c. Next to the `.bn` marker (reuse the existing marker-discovery walk in
      `app.go`), as `bn.toml`/`bn.yaml`.
   d. `$XDG_CONFIG_HOME/bn/config.{toml,yaml}` (fallback to
      `~/.config/bn/...`).
   First match wins; we do **not** merge multiple files.
3. **Targeted env overrides** (optional, thin): `BN_STATUS_DEFAULT` to override
   just the default status. Full-vocabulary override stays file-only to avoid
   unparseable mega-env-vars.

A partial config file is allowed: any omitted key inherits the built-in default
(e.g. a file that sets only `default` keeps the default vocabulary). Merge is
key-level, not deep.

## Library choice

**Decision: dispatch by file extension using two well-scoped encoders** —
`github.com/BurntSushi/toml` for `.toml` and `gopkg.in/yaml.v3` for
`.yaml`/`.yml`. Rationale:

- The project curates `go.mod` tightly; these are two small, ubiquitous,
  well-audited libraries with no heavy transitive trees.
- We need *decode-a-file-into-a-struct*, nothing more. No live-reload, no remote
  providers, no flag binding.

**Alternative considered:** `github.com/knadh/koanf/v2` (one library, both
formats, plus native env-overlay merging). Cleaner if we later want layered
config (defaults → file → env) handled by the library instead of by hand.
Recommended *only if* the env-override surface grows beyond `BN_STATUS_DEFAULT`.
For v1 the two-encoder approach is less dependency for the same result.

> Confirm the final choice with the maintainer before adding to `go.mod`
> (`02` validation step in `06-rollout-and-testing.md`).

## Validation rules (loaded once, at startup)

After decoding, validate the config and **fail fast** with a clear error if:

- `statuses` is empty.
- `default` ∉ `statuses`.
- any member of `active`/`terminal` ∉ `statuses`.
- `active` and `terminal` overlap (a status cannot be both dispatchable and
  done).
- (when `transitions` present) any key or target ∉ `statuses`.

A bad config should stop `bn` at startup with a single actionable message, not
silently fall back — silent fallback would mask deployment mistakes.

## Threading into the app

`WorkflowConfig` is resolved once during `appState` construction (alongside
`storeConfigFromEnv`) and stored on `appState`. Every command reads it from
there. The store needs it too (for `isValidIssueState` and `ReadyIssues`
defaults); pass it into the store at construction, or pass the relevant
sub-slices per call as `ReadyIssues` already accepts `terminalStates`/
`activeStates` arguments (`store/store.go:449`). See `04-code-changes.md` for the
exact wiring and the read-tolerant/write-strict split.
