package vault

import (
	"sort"
	"strings"

	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/plan"
)

// PlanByID returns one plan by stable id.
func (ix *Index) PlanByID(id string) (*plan.Plan, bool) { p, ok := ix.Plans[id]; return p, ok }

// ProjectPlans returns plans for a project, sorted by updated descending then id.
func (ix *Index) ProjectPlans(project string) []*plan.Plan {
	var out []*plan.Plan
	for _, p := range ix.Plans {
		n, ok := ix.ByPath[p.Path]
		if ok && (project == "" || n.Project == project) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Ready returns the issues eligible for dispatch: status is Active per the
// issue's project workflow, not archived, not an epic with children, and
// every blocked_by target resolves to an issue that is Terminal per the
// blocker's project workflow (an unresolved blocker blocks). Sorted by
// priority ascending, then created ascending, then id. With project != "",
// only that project's issues are considered; all ignores project.
func (ix *Index) Ready(project string, all bool) []*issue.Issue {
	p := project
	if all {
		p = ""
	}
	var out []*issue.Issue
	for _, iss := range ix.ProjectIssues(p, false) {
		wf := ix.WorkflowFor(iss.Project)
		if !wf.IsActive(iss.Status) {
			continue
		}
		if iss.Type == "epic" && len(ix.Children(iss.ID)) > 0 {
			continue
		}
		if ix.hasBlocker(iss) {
			continue
		}
		out = append(out, iss)
	}
	sortIssues(out)
	return out
}

func (ix *Index) hasBlocker(iss *issue.Issue) bool {
	for _, b := range iss.BlockedBy {
		_, blocker, ok := ix.ResolveIssueRef(b.Raw)
		if !ok {
			return true
		}
		bwf := ix.WorkflowFor(blocker.Project)
		if !bwf.IsTerminal(blocker.Status) {
			return true
		}
	}
	return false
}

// BlockedIssue is one issue with at least one unsatisfied blocker.
type BlockedIssue struct {
	Issue    *issue.Issue
	Blockers []string // ids or raw targets
}

// Blocked returns issues (active or hold, not archived) with at least one
// non-terminal or unresolved blocker.
func (ix *Index) Blocked(project string, all bool) []BlockedIssue {
	p := project
	if all {
		p = ""
	}
	var out []BlockedIssue
	for _, iss := range ix.ProjectIssues(p, false) {
		wf := ix.WorkflowFor(iss.Project)
		if wf.IsTerminal(iss.Status) {
			continue
		}
		var blockers []string
		for _, b := range iss.BlockedBy {
			note, ok := ix.Lookup(b.Target)
			if !ok || note.Issue == nil {
				blockers = append(blockers, b.Target)
				continue
			}
			bwf := ix.WorkflowFor(note.Issue.Project)
			if !bwf.IsTerminal(note.Issue.Status) {
				blockers = append(blockers, note.Issue.ID)
			}
		}
		if len(blockers) > 0 {
			out = append(out, BlockedIssue{Issue: iss, Blockers: blockers})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Issue.ID < out[j].Issue.ID })
	return out
}

// Children returns the issues whose parent resolves to id, across projects,
// sorted by id.
func (ix *Index) Children(id string) []*issue.Issue {
	var out []*issue.Issue
	for _, iss := range ix.Issues {
		if iss.Parent.IsZero() {
			continue
		}
		note, ok := ix.Lookup(iss.Parent.Target)
		if !ok || note.Issue == nil {
			continue
		}
		if note.Issue.ID == id {
			out = append(out, iss)
		}
	}
	sortByID(out)
	return out
}

// Parents returns the resolved parent chain of id, nearest first.
func (ix *Index) Parents(id string) []*issue.Issue {
	iss, ok := ix.IssueByID(id)
	if !ok {
		return nil
	}
	var out []*issue.Issue
	visited := map[string]bool{id: true}
	for !iss.Parent.IsZero() {
		note, ok := ix.Lookup(iss.Parent.Target)
		if !ok || note.Issue == nil {
			break
		}
		if visited[note.Issue.ID] {
			break
		}
		visited[note.Issue.ID] = true
		out = append(out, note.Issue)
		iss = note.Issue
	}
	return out
}

// Blockers resolves iss's blocked_by list against the index.
func (ix *Index) Blockers(iss *issue.Issue) (resolved []*issue.Issue, unresolved []string) {
	for _, b := range iss.BlockedBy {
		_, blocker, ok := ix.ResolveIssueRef(b.Raw)
		if ok {
			resolved = append(resolved, blocker)
		} else {
			unresolved = append(unresolved, b.Target)
		}
	}
	return resolved, unresolved
}

// GraphNode is one issue in a dependency graph.
type GraphNode struct {
	ID       string
	Title    string
	Status   string
	Type     string
	Project  string
	Priority int
	Archived bool
}

// GraphEdge is one edge in a dependency graph. Kind is "blocks" (issue ->
// blocker) or "parent" (child -> parent).
type GraphEdge struct {
	From string
	To   string
	Kind string
}

// Graph returns the nodes and edges of the dependency graph for project (""
// = all projects; all ignores project). Cross-project edges are included:
// an edge's target issue is added as a node even when it falls outside the
// project filter.
func (ix *Index) Graph(project string, all bool) ([]GraphNode, []GraphEdge) {
	p := project
	if all {
		p = ""
	}
	base := ix.ProjectIssues(p, true)
	nodeSet := make(map[string]*issue.Issue, len(base))
	for _, iss := range base {
		nodeSet[iss.ID] = iss
	}

	var edges []GraphEdge
	for _, iss := range base {
		for _, b := range iss.BlockedBy {
			note, ok := ix.Lookup(b.Target)
			if !ok || note.Issue == nil {
				continue
			}
			if _, exists := nodeSet[note.Issue.ID]; !exists {
				nodeSet[note.Issue.ID] = note.Issue
			}
			edges = append(edges, GraphEdge{From: iss.ID, To: note.Issue.ID, Kind: "blocks"})
		}
		if !iss.Parent.IsZero() {
			note, ok := ix.Lookup(iss.Parent.Target)
			if ok && note.Issue != nil {
				if _, exists := nodeSet[note.Issue.ID]; !exists {
					nodeSet[note.Issue.ID] = note.Issue
				}
				edges = append(edges, GraphEdge{From: iss.ID, To: note.Issue.ID, Kind: "parent"})
			}
		}
	}

	nodes := make([]GraphNode, 0, len(nodeSet))
	for _, iss := range nodeSet {
		nodes = append(nodes, GraphNode{
			ID: iss.ID, Title: iss.Title, Status: iss.Status, Type: iss.Type,
			Project: iss.Project, Priority: iss.Priority, Archived: iss.Archived,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		return edges[i].To < edges[j].To
	})
	return nodes, edges
}

// Cycles returns every cycle in the "blocks" graph (issue -> blocker edges
// only; parent edges cannot block), as sorted lists of ids. Only cycles of
// length >= 2, plus self-loops, are reported.
func (ix *Index) Cycles() [][]string {
	adj := map[string][]string{}
	for _, iss := range ix.Issues {
		for _, b := range iss.BlockedBy {
			note, ok := ix.Lookup(b.Target)
			if ok && note.Issue != nil {
				adj[iss.ID] = append(adj[iss.ID], note.Issue.ID)
			}
		}
	}

	var (
		index    int
		indices  = map[string]int{}
		lowlink  = map[string]int{}
		onStack  = map[string]bool{}
		stack    []string
		sccs     [][]string
		strongly func(v string)
	)
	strongly = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, seen := indices[w]; !seen {
				strongly(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				n := len(stack) - 1
				w := stack[n]
				stack = stack[:n]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}

	ids := make([]string, 0, len(ix.Issues))
	for id := range ix.Issues {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, seen := indices[id]; !seen {
			strongly(id)
		}
	}

	var out [][]string
	for _, scc := range sccs {
		switch {
		case len(scc) >= 2:
			sort.Strings(scc)
			out = append(out, scc)
		case len(scc) == 1:
			v := scc[0]
			for _, w := range adj[v] {
				if w == v {
					out = append(out, []string{v})
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], ",") < strings.Join(out[j], ",")
	})
	return out
}

// Hit is one Search result.
type Hit struct {
	Kind     Kind
	ID       string // issue id or basename
	Basename string
	Title    string
	Project  string
	Path     string
	Score    int
}

// SearchOptions controls typed search. Archived handoffs are opt-in because
// they are historical continuation context rather than current work.
type SearchOptions struct {
	Kinds                   []Kind
	IncludeArchivedHandoffs bool
}

// Search does a case-insensitive substring search over title, id, labels,
// and body/description, ranked title match first, then id, then the rest.
// kinds nil means all kinds.
func (ix *Index) Search(q string, kinds []Kind) []Hit {
	return ix.SearchWithOptions(q, SearchOptions{Kinds: kinds})
}

func (ix *Index) SearchWithOptions(q string, opts SearchOptions) []Hit {
	ql := strings.ToLower(strings.TrimSpace(q))
	if ql == "" {
		return nil
	}
	var kindSet map[Kind]bool
	if len(opts.Kinds) > 0 {
		kindSet = make(map[Kind]bool, len(opts.Kinds))
		for _, k := range opts.Kinds {
			kindSet[k] = true
		}
	}

	var hits []Hit
	for _, n := range ix.order {
		if n.Handoff != nil && n.Handoff.Archived && !opts.IncludeArchivedHandoffs {
			continue
		}
		if kindSet != nil && !kindSet[n.Kind] {
			continue
		}
		id := ""
		if n.Kind == KindIssue && n.Issue != nil {
			id = n.Issue.ID
		}
		if n.Kind == KindHandoff && n.Handoff != nil {
			id = n.Handoff.ID
		}
		if n.Kind == KindRequest && n.Request != nil {
			id = n.Request.ID
		}
		if n.Kind == KindPlan && n.Plan != nil {
			id = n.Plan.ID
		}

		score := 0
		switch {
		case strings.Contains(strings.ToLower(n.Title), ql):
			score = 3
		case id != "" && strings.Contains(strings.ToLower(id), ql):
			score = 2
		default:
			for _, tag := range n.Tags {
				if strings.Contains(strings.ToLower(tag), ql) {
					score = 1
					break
				}
			}
			if score == 0 && strings.Contains(strings.ToLower(noteSearchBody(n)), ql) {
				score = 1
			}
		}
		if score == 0 {
			continue
		}

		hitID := id
		if hitID == "" {
			hitID = n.Basename
		}
		hits = append(hits, Hit{
			Kind: n.Kind, ID: hitID, Basename: n.Basename, Title: n.Title,
			Project: n.Project, Path: n.Path, Score: score,
		})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Basename < hits[j].Basename
	})
	return hits
}

func noteSearchBody(n *Note) string {
	switch n.Kind {
	case KindIssue:
		if n.Issue != nil {
			return n.Issue.Description + "\n" + n.Issue.Body
		}
	case KindMemory:
		if n.Memory != nil {
			return n.Memory.Body
		}
	case KindDoc:
		return n.docBody
	case KindRequest:
		if n.Request != nil {
			return n.Request.Body
		}
	case KindPlan:
		if n.Plan != nil {
			body := n.Plan.Body + "\n" + n.Plan.Summary.Outcome + "\n" + n.Plan.Summary.AffectedAreas + "\n" + n.Plan.Summary.ExecutionOrder + "\n" + n.Plan.Summary.Risks
			for _, section := range n.Plan.SectionBodies {
				body += "\n" + section.Markdown
			}
			return body
		}
	case KindHandoff:
		if n.Handoff != nil {
			return n.Handoff.Body
		}
	}
	return ""
}

// HandoffByID returns a live or archived handoff by stable id.
func (ix *Index) HandoffByID(id string) (*issue.Handoff, bool) {
	h, ok := ix.Handoffs[id]
	return h, ok
}

// ProjectHandoffs returns newest-first handoffs for a project (or all).
func (ix *Index) ProjectHandoffs(project string, includeArchived bool) []*issue.Handoff {
	out := make([]*issue.Handoff, 0, len(ix.Handoffs))
	for _, h := range ix.Handoffs {
		if (project == "" || h.Project == project) && (includeArchived || !h.Archived) {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// IssueBacklinks filters only archived handoff sources; raw backlinks remain complete.
func (ix *Index) IssueBacklinks(basename string, includeArchivedHandoffs bool) []LinkRef {
	out := []LinkRef{}
	for _, r := range ix.Backlinks[basename] {
		if n := ix.Notes[r.From]; n != nil && n.Handoff != nil && n.Handoff.Archived && !includeArchivedHandoffs {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sortIssues(list []*issue.Issue) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.ID < b.ID
	})
}
