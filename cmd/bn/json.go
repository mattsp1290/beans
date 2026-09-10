package main

import (
	"sort"
	"time"

	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

// issueJSON is the stable --json shape of an issue.
type issueJSON struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Priority    int            `json:"priority"`
	Labels      []string       `json:"labels"`
	Assignee    string         `json:"assignee"`
	Parent      string         `json:"parent"`
	BlockedBy   []string       `json:"blocked_by"`
	URL         string         `json:"url"`
	Created     time.Time      `json:"created"`
	Updated     time.Time      `json:"updated"`
	Project     string         `json:"project"`
	Path        string         `json:"path"`
	Archived    bool           `json:"archived"`
	Description string         `json:"description,omitempty"`
	Log         []logJSON      `json:"log,omitempty"`
	Children    []string       `json:"children,omitempty"`
	Backlinks   []backlinkJSON `json:"backlinks,omitempty"`
	Blockers    []blockerJSON  `json:"blockers,omitempty"`
}

type logJSON struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Repo   string    `json:"repo,omitempty"`
	SHA    string    `json:"sha,omitempty"`
	Branch string    `json:"branch,omitempty"`
	Event  string    `json:"event"`
	Raw    string    `json:"raw,omitempty"`
}

type backlinkJSON struct {
	From string `json:"from"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

type blockerJSON struct {
	ID      string `json:"id"`
	Status  string `json:"status,omitempty"`
	Project string `json:"project,omitempty"`
	Title   string `json:"title,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

// resolveID maps a link target to an issue id through the index; an
// unresolved target is passed through as written.
func resolveID(ix *vault.Index, target string) string {
	if ix != nil {
		if n, ok := ix.Lookup(target); ok && n.Issue != nil {
			return n.Issue.ID
		}
	}
	return target
}

// toIssueJSON builds the summary shape (no description, log, or graph).
func toIssueJSON(ix *vault.Index, iss *issue.Issue) issueJSON {
	out := issueJSON{
		ID: iss.ID, Title: iss.Title, Type: iss.Type, Status: iss.Status, Priority: iss.Priority,
		Labels: iss.Labels, Assignee: iss.Assignee, URL: iss.URL, Created: iss.Created, Updated: iss.Updated,
		Project: iss.Project, Path: iss.Path, Archived: iss.Archived, BlockedBy: []string{},
	}
	if out.Labels == nil {
		out.Labels = []string{}
	}
	if !iss.Parent.IsZero() {
		out.Parent = resolveID(ix, iss.Parent.Target)
	}
	for _, b := range iss.BlockedBy {
		out.BlockedBy = append(out.BlockedBy, resolveID(ix, b.Target))
	}
	return out
}

// toIssueDetailJSON adds description, log, children, backlinks, blockers.
func toIssueDetailJSON(ix *vault.Index, iss *issue.Issue) issueJSON {
	out := toIssueJSON(ix, iss)
	out.Description = iss.Description
	for _, e := range iss.Log {
		out.Log = append(out.Log, logJSON{At: e.At, Actor: e.Actor, Repo: e.Repo, SHA: e.SHA, Branch: e.Branch, Event: e.Event, Raw: e.Raw})
	}
	if ix == nil {
		return out
	}
	for _, c := range ix.Children(iss.ID) {
		out.Children = append(out.Children, c.ID)
	}
	basename := noteBasename(iss)
	refs := append([]vault.LinkRef(nil), ix.Backlinks[basename]...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].From < refs[j].From })
	for _, r := range refs {
		bl := backlinkJSON{From: r.From, Kind: string(r.Kind)}
		if n, ok := ix.Notes[r.From]; ok {
			bl.Path = n.Path
			if n.Issue != nil {
				bl.From = n.Issue.ID
			}
		}
		out.Backlinks = append(out.Backlinks, bl)
	}
	resolved, unresolved := ix.Blockers(iss)
	for _, b := range resolved {
		out.Blockers = append(out.Blockers, blockerJSON{ID: b.ID, Status: b.Status, Project: b.Project, Title: b.Title})
	}
	for _, u := range unresolved {
		out.Blockers = append(out.Blockers, blockerJSON{ID: u, Missing: true})
	}
	return out
}

func noteBasename(iss *issue.Issue) string {
	p := iss.Path
	if i := lastSlash(p); i >= 0 {
		p = p[i+1:]
	}
	if len(p) > 3 && p[len(p)-3:] == ".md" {
		p = p[:len(p)-3]
	}
	return p
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
