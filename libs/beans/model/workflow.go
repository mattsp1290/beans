package model

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// WorkflowConfig is the runtime source of truth for the issue-status vocabulary
// and its classification into dispatch buckets. It replaces the status sets that
// were previously hardcoded across the store and CLI layers, and is loaded from
// a deployment config file (TOML/YAML) with built-in defaults.
//
// The three buckets mirror the model documented on IssueState:
//
//   - Active   — eligible for dispatch (surfaced by "bn ready").
//   - Terminal — treated as "done": satisfies blockers, triggers cleanup.
//   - Hold     — any valid status that is neither Active nor Terminal. Held in
//     flight: not dispatched, not counted done. Computed, never stored.
//
// The system is read-tolerant / write-strict: callers must reject *writes* of a
// status outside Statuses, but reading a row whose status is not in the config
// must never panic. Unknown statuses are invalid, non-active, and non-terminal,
// so they are conservatively excluded from dispatch and never counted done.
type WorkflowConfig struct {
	// Statuses is the ordered status vocabulary. Order drives display.
	Statuses []IssueState
	// Default is the status assigned to newly created issues.
	Default IssueState
	// Active lists dispatchable statuses (bn ready).
	Active []IssueState
	// Terminal lists statuses treated as done (satisfy blockers, cleanup).
	Terminal []IssueState
	// Transitions optionally restricts legal status moves. Reserved for a future
	// phase; nil/empty means transitions are unrestricted (v1 behavior).
	Transitions map[IssueState][]IssueState
}

// DefaultWorkflowConfig returns the built-in vocabulary used when no config file
// is present. It is a superset of the legacy five states plus the three
// ready_for_* hold states, with identical active/terminal classification.
func DefaultWorkflowConfig() WorkflowConfig {
	return WorkflowConfig{
		Statuses: []IssueState{
			"open",
			"in_progress",
			"ready_for_review",
			"ready_for_validation",
			"ready_for_merge",
			"blocked",
			"closed",
			"done",
		},
		Default:  "open",
		Active:   []IssueState{"open"},
		Terminal: []IssueState{"closed", "done"},
	}
}

// IsValid reports whether s is part of the configured vocabulary.
func (w WorkflowConfig) IsValid(s IssueState) bool { return containsState(w.Statuses, s) }

// IsActive reports whether s is a dispatchable status.
func (w WorkflowConfig) IsActive(s IssueState) bool { return containsState(w.Active, s) }

// IsTerminal reports whether s is a terminal status.
func (w WorkflowConfig) IsTerminal(s IssueState) bool { return containsState(w.Terminal, s) }

// IsHold reports whether s is a valid status that is neither active nor
// terminal. Unknown (non-vocabulary) statuses are NOT hold — use IsValid first
// when the distinction matters.
func (w WorkflowConfig) IsHold(s IssueState) bool {
	return w.IsValid(s) && !w.IsActive(s) && !w.IsTerminal(s)
}

// DefaultState returns the new-issue default, falling back to "open" when a
// zero-value WorkflowConfig is used defensively.
func (w WorkflowConfig) DefaultState() IssueState {
	if w.Default == "" {
		return "open"
	}
	return w.Default
}

// StatusNames returns the configured statuses as plain strings in vocabulary
// order, for help text and error messages.
func (w WorkflowConfig) StatusNames() []string {
	out := make([]string, len(w.Statuses))
	for i, s := range w.Statuses {
		out[i] = string(s)
	}
	return out
}

// Validate enforces the structural invariants the loader relies on. It returns
// a single actionable error so a bad deployment config fails fast at startup.
func (w WorkflowConfig) Validate() error {
	if len(w.Statuses) == 0 {
		return fmt.Errorf("workflow: statuses must not be empty")
	}

	known := make(map[IssueState]bool, len(w.Statuses))
	for _, s := range w.Statuses {
		if strings.TrimSpace(string(s)) == "" {
			return fmt.Errorf("workflow: statuses must not contain empty values")
		}
		if known[s] {
			return fmt.Errorf("workflow: duplicate status %q", s)
		}
		known[s] = true
	}

	if !known[w.Default] {
		return fmt.Errorf("workflow: default status %q is not in statuses", w.Default)
	}
	for _, s := range w.Active {
		if !known[s] {
			return fmt.Errorf("workflow: active status %q is not in statuses", s)
		}
	}
	for _, s := range w.Terminal {
		if !known[s] {
			return fmt.Errorf("workflow: terminal status %q is not in statuses", s)
		}
	}

	// A status cannot be both dispatchable and done.
	terminal := make(map[IssueState]bool, len(w.Terminal))
	for _, s := range w.Terminal {
		terminal[s] = true
	}
	overlap := make([]string, 0)
	for _, s := range w.Active {
		if terminal[s] {
			overlap = append(overlap, string(s))
		}
	}
	if len(overlap) > 0 {
		sort.Strings(overlap)
		return fmt.Errorf("workflow: status(es) cannot be both active and terminal: %s", strings.Join(overlap, ", "))
	}

	for from, tos := range w.Transitions {
		if !known[from] {
			return fmt.Errorf("workflow: transition source %q is not in statuses", from)
		}
		for _, to := range tos {
			if !known[to] {
				return fmt.Errorf("workflow: transition target %q (from %q) is not in statuses", to, from)
			}
		}
	}
	return nil
}

func containsState(states []IssueState, target IssueState) bool {
	return slices.Contains(states, target)
}
