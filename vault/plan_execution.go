package vault

import (
	"sort"

	"github.com/mattsp1290/beans/issue"
)

// PlanBindingKind describes what a graph ref names in the current hub.
type PlanBindingKind string

const (
	PlanBindingUnlinked     PlanBindingKind = "unlinked"
	PlanBindingReference    PlanBindingKind = "reference"
	PlanBindingIssue        PlanBindingKind = "issue"
	PlanBindingMissingIssue PlanBindingKind = "missing_issue"
)

type PlanWorkState string

const (
	PlanWorkRunnable   PlanWorkState = "runnable"
	PlanWorkInProgress PlanWorkState = "in_progress"
	PlanWorkHeld       PlanWorkState = "held"
	PlanWorkBlocked    PlanWorkState = "blocked"
	PlanWorkDone       PlanWorkState = "done"
	PlanWorkMissing    PlanWorkState = "missing"
)

type PlanExecutionIssue struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
	Archived bool   `json:"archived"`
}
type PlanExecutionBlocker struct {
	Target  string `json:"target"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Project string `json:"project"`
	Missing bool   `json:"missing"`
}
type PlanExecutionNode struct {
	NodeID     string                 `json:"node_id"`
	Label      string                 `json:"label"`
	Kind       string                 `json:"kind"`
	Ref        string                 `json:"ref"`
	Binding    PlanBindingKind        `json:"binding"`
	WorkState  PlanWorkState          `json:"work_state"`
	HoldReason string                 `json:"hold_reason"`
	Issue      *PlanExecutionIssue    `json:"issue,omitempty"`
	Blockers   []PlanExecutionBlocker `json:"blockers"`
}
type PlanExecutionCounts struct {
	Unlinked       int `json:"unlinked"`
	Reference      int `json:"reference"`
	Issue          int `json:"issue"`
	MissingIssue   int `json:"missing_issue"`
	Missing        int `json:"missing"`
	Runnable       int `json:"runnable"`
	InProgress     int `json:"in_progress"`
	Held           int `json:"held"`
	Blocked        int `json:"blocked"`
	Done           int `json:"done"`
	DistinctIssues int `json:"distinct_issues"`
}
type PlanExecution struct {
	PlanID            string              `json:"plan_id"`
	Title             string              `json:"title"`
	Project           string              `json:"project"`
	LifecycleStatus   string              `json:"lifecycle_status"`
	ExecutionState    string              `json:"execution_state"`
	LifecycleMismatch bool                `json:"lifecycle_mismatch"`
	Counts            PlanExecutionCounts `json:"counts"`
	Nodes             []PlanExecutionNode `json:"nodes"`
}

// ResolveIssueRef resolves wikilink syntax as an issue only. Exact IDs win
// over a colliding generic note basename.
func (ix *Index) ResolveIssueRef(raw string) (string, *issue.Issue, bool) {
	target := issue.ParseLink(raw).Target
	if iss, ok := ix.IssueByID(target); ok {
		return target, iss, true
	}
	if n, ok := ix.Lookup(target); ok && n.Issue != nil {
		return target, n.Issue, true
	}
	return target, nil, false
}

func (ix *Index) executionBlockers(iss *issue.Issue) []PlanExecutionBlocker {
	var out []PlanExecutionBlocker
	for _, link := range iss.BlockedBy {
		target, blocker, ok := ix.ResolveIssueRef(link.Raw)
		if !ok {
			out = append(out, PlanExecutionBlocker{Target: target, Missing: true})
			continue
		}
		if !ix.WorkflowFor(blocker.Project).IsTerminal(blocker.Status) {
			out = append(out, PlanExecutionBlocker{Target: target, ID: blocker.ID, Title: blocker.Title, Status: blocker.Status, Project: blocker.Project})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func (ix *Index) executionState(iss *issue.Issue) (PlanWorkState, string, []PlanExecutionBlocker) {
	wf := ix.WorkflowFor(iss.Project)
	if wf.IsTerminal(iss.Status) {
		return PlanWorkDone, "", nil
	}
	blockers := ix.executionBlockers(iss)
	if len(blockers) > 0 {
		return PlanWorkBlocked, "", blockers
	}
	if iss.Archived {
		return PlanWorkHeld, "archived", nil
	}
	if iss.Type == "epic" && len(ix.Children(iss.ID)) > 0 {
		return PlanWorkHeld, "epic_has_children", nil
	}
	if iss.Status == "in_progress" {
		return PlanWorkInProgress, "", nil
	}
	if wf.IsActive(iss.Status) {
		return PlanWorkRunnable, "", nil
	}
	return PlanWorkHeld, "workflow_hold", nil
}

// PlanExecution derives execution from current indexed issues without
// changing a stored plan lifecycle.
func (ix *Index) PlanExecution(id string) (PlanExecution, bool) {
	p, ok := ix.PlanByID(id)
	if !ok {
		return PlanExecution{}, false
	}
	project := ""
	if n := ix.ByPath[p.Path]; n != nil {
		project = n.Project
	}
	out := PlanExecution{PlanID: p.ID, Title: p.Title, Project: project, LifecycleStatus: string(p.Status), Nodes: make([]PlanExecutionNode, 0, len(p.Graph.Nodes))}
	distinct := map[string]bool{}
	for _, g := range p.Graph.Nodes {
		n := PlanExecutionNode{NodeID: g.ID, Label: g.Label, Kind: g.Kind, Ref: g.Ref, Blockers: []PlanExecutionBlocker{}}
		if g.Ref == "" {
			n.Binding = PlanBindingUnlinked
			out.Counts.Unlinked++
			out.Nodes = append(out.Nodes, n)
			continue
		}
		target, iss, resolved := ix.ResolveIssueRef(g.Ref)
		if resolved {
			n.Binding = PlanBindingIssue
			n.Issue = &PlanExecutionIssue{ID: iss.ID, Title: iss.Title, Status: iss.Status, Priority: iss.Priority, Project: iss.Project, Archived: iss.Archived}
			n.WorkState, n.HoldReason, n.Blockers = ix.executionState(iss)
			out.Counts.Issue++
			distinct[iss.ID] = true
			switch n.WorkState {
			case PlanWorkRunnable:
				out.Counts.Runnable++
			case PlanWorkInProgress:
				out.Counts.InProgress++
			case PlanWorkHeld:
				out.Counts.Held++
			case PlanWorkBlocked:
				out.Counts.Blocked++
			case PlanWorkDone:
				out.Counts.Done++
			}
		} else if issue.ValidID(target) {
			n.Binding = PlanBindingMissingIssue
			n.WorkState = PlanWorkMissing
			out.Counts.MissingIssue++
			out.Counts.Missing++
		} else {
			n.Binding = PlanBindingReference
			out.Counts.Reference++
		}
		out.Nodes = append(out.Nodes, n)
	}
	out.Counts.DistinctIssues = len(distinct)
	c := out.Counts
	switch {
	case c.Issue+c.MissingIssue == 0:
		out.ExecutionState = "untracked"
	case c.MissingIssue > 0:
		out.ExecutionState = "degraded"
	case c.InProgress > 0:
		out.ExecutionState = "in_progress"
	case c.Runnable > 0:
		out.ExecutionState = "runnable"
	case c.Blocked > 0:
		out.ExecutionState = "blocked"
	case c.Issue > 0 && c.Done == c.Issue:
		out.ExecutionState = "done"
	default:
		out.ExecutionState = "held"
	}
	out.LifecycleMismatch = (p.Status == "complete" && out.ExecutionState != "done" && out.ExecutionState != "untracked") || (p.Status != "complete" && out.ExecutionState == "done")
	return out, true
}
