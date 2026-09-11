// Package plan defines the portable, validated plan bundle stored by Beans.
package plan

import "time"

type Status string

const (
	StatusDraft    Status = "draft"
	StatusBlocked  Status = "blocked"
	StatusReady    Status = "ready"
	StatusComplete Status = "complete"
)

func (s Status) Valid() bool {
	return s == StatusDraft || s == StatusBlocked || s == StatusReady || s == StatusComplete
}

// Plan is the canonical manifest. Body is the Markdown following frontmatter.
type Plan struct {
	ID       string
	Aliases  []string
	Title    string
	Slug     string
	Status   Status
	Created  time.Time
	Updated  time.Time
	Sections []string
	// SectionBodies is validated ordered content supplied by bundle loading.
	// It is not serialized into the manifest.
	SectionBodies []Section
	Body          string
	Path          string
	Summary       Summary
	Graph         ChangeGraph
}

type Section struct{ Path, Markdown string }

type Summary struct {
	Outcome, AffectedAreas, ExecutionOrder, Risks, ChangeGraph string
}

type ChangeGraph struct {
	Version int         `json:"version"`
	Nodes   []GraphNode `json:"nodes"`
	Edges   []GraphEdge `json:"edges"`
}
type GraphNode struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
	Kind  string `yaml:"kind" json:"kind"`
	Ref   string `yaml:"ref,omitempty" json:"ref,omitempty"`
}
type GraphEdge struct {
	From  string `yaml:"from" json:"from"`
	To    string `yaml:"to" json:"to"`
	Kind  string `yaml:"kind" json:"kind"`
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
}

// Bundle is an aggregate plan and its explicitly ordered section files.
type Bundle struct {
	Plan     *Plan
	Sections []Section
	Root     string
}

// BundleSnapshot is an immutable, deterministic file map captured before publication.
type BundleSnapshot struct{ Files map[string][]byte }

type ValidationIssue struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationError struct{ Issues []ValidationIssue }

func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "invalid plan"
	}
	return e.Issues[0].Path + ": " + e.Issues[0].Message
}
