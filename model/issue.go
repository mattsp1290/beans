package model

import "time"

// Priority is an ordinal where lower (non-zero) values mean higher urgency.
//
// The Go zero value (PriorityUnset) deliberately means "tracker did not supply
// a priority" — a default-constructed Issue or one with a missing field will
// fail forward instead of silently being treated as the most urgent work.
// Tracker adapters are responsible for mapping the external numeric scheme
// (e.g., beads' 0=critical..4=backlog) into this internal enum at the parse
// boundary; passing the raw external integer through is a bug.
//
// PriorityUnset sorts after every concrete priority in the orchestrator's
// candidate sort (per spec §9.1: "priority asc, null last").
type Priority int

const (
	// PriorityUnset is the zero value: "tracker did not supply a priority."
	// It must sort after every concrete priority.
	PriorityUnset Priority = iota
	PriorityCritical
	PriorityHigh
	PriorityMedium
	PriorityLow
	PriorityBacklog
)

// Valid reports whether p is one of the defined priority levels (including
// PriorityUnset). Out-of-range values from external trackers should be
// rejected at the tracker-adapter boundary, not silently coerced.
func (p Priority) Valid() bool {
	return p >= PriorityUnset && p <= PriorityBacklog
}

// IssueState is the tracker-defined lifecycle state of an Issue.
//
// The set of legal states is tracker-specific, but Symphony's orchestrator
// reasons about three buckets via WORKFLOW.md configuration:
//
//   - active states  — eligible for dispatch (e.g. "open", "todo").
//   - terminal states — workspace cleanup target (e.g. "closed", "done").
//   - everything else — held without cleanup.
//
// We model IssueState as a typed string rather than an enum so trackers with
// custom state names (Linear, JIRA workflows) flow through unchanged. The
// orchestrator classifies each state via WorkflowConfig at runtime.
type IssueState string

// Issue is one tracker work item as observed at fetch time. It is a snapshot
// — the orchestrator may re-fetch and overwrite it on the next reconcile tick.
// Once handed to a worker via dispatch, the worker MUST treat its Issue value
// as read-only; concurrent re-fetches happen on a fresh value held by the
// orchestrator's reconcile loop.
//
// Field naming mirrors Symphony spec §4. Time fields are normalized to UTC by
// the tracker adapter before construction.
type Issue struct {
	// ID is the tracker's stable primary key (e.g., "beans-0cn").
	// Used for dispatch coordination, claim tracking, and persistence FK.
	ID string `json:"id" db:"id"`

	// Identifier is the human-facing display ID (often the same as ID for
	// beads, but may differ for trackers like Linear where ID is opaque).
	Identifier string `json:"identifier" db:"identifier"`

	Title       string `json:"title" db:"title"`
	Description string `json:"description" db:"description"`

	Priority Priority   `json:"priority" db:"priority"`
	State    IssueState `json:"state" db:"state"`

	// BranchName is the tracker-suggested git branch for this issue, if any.
	// Empty string when the tracker does not provide one.
	BranchName string `json:"branch_name,omitempty" db:"branch_name"`

	// URL is the tracker's web link to this issue, if any. Empty for trackers
	// without a web UI (e.g., beads when no project URL is configured).
	URL string `json:"url,omitempty" db:"url"`

	Labels    []string `json:"labels,omitempty" db:"labels"`
	BlockedBy []string `json:"blocked_by,omitempty" db:"blocked_by"`

	// Repo is the structured repository target for multi-repo workspace
	// routing. Nil means the issue is repo-less and should use the legacy
	// workspace behavior.
	Repo *RepoTarget `json:"repo,omitempty" db:"repo"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// RepoTarget is the repository routing snapshot attached to an issue.
type RepoTarget struct {
	ID             string         `json:"id" db:"id"`
	Slug           string         `json:"slug" db:"slug"`
	RemoteURL      string         `json:"remote_url" db:"remote_url"`
	DefaultBranch  string         `json:"default_branch" db:"default_branch"`
	RequestedRef   string         `json:"requested_ref,omitempty" db:"requested_ref"`
	BaseRef        string         `json:"base_ref,omitempty" db:"base_ref"`
	WorkBranch     string         `json:"work_branch,omitempty" db:"work_branch"`
	WorktreeSubdir string         `json:"worktree_subdir,omitempty" db:"worktree_subdir"`
	CloneStrategy  string         `json:"clone_strategy" db:"clone_strategy"`
	AuthRef        string         `json:"auth_ref" db:"auth_ref"`
	Metadata       map[string]any `json:"metadata,omitempty" db:"metadata"`
}
