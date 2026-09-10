package ops

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
)

// BDRecord is one line of a bd export. Memory records carry _type: "memory".
type BDRecord struct {
	Type         string   `json:"_type"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
	Priority     int      `json:"priority"`
	IssueType    string   `json:"issue_type"`
	Labels       []string `json:"labels"`
	Assignee     string   `json:"assignee"`
	Owner        string   `json:"owner"`
	CreatedBy    string   `json:"created_by"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	ClosedAt     string   `json:"closed_at"`
	CloseReason  string   `json:"close_reason"`
	Notes        string   `json:"notes"`
	Acceptance   string   `json:"acceptance_criteria"`
	Design       string   `json:"design"`
	BranchName   string   `json:"branch_name"`
	URL          string   `json:"url"`
	Dependencies []BDDep  `json:"dependencies"`
	Key          string   `json:"key"`
	Value        string   `json:"value"`
}

// BDDep is one edge of dependencies[].
type BDDep struct {
	IssueID   string `json:"issue_id"`
	DependsOn string `json:"depends_on_id"`
	Type      string `json:"type"`
}

// ImportReport is the dry-run and result report of bn import bd.
type ImportReport struct {
	Issues     int               `json:"issues"`
	Memories   int               `json:"memories"`
	Blocks     int               `json:"blocks"`
	Parents    int               `json:"parents"`
	Archived   int               `json:"archived"`
	Rejected   []string          `json:"rejected"`
	Warnings   []string          `json:"warnings"`
	Unresolved []string          `json:"unresolved"`
	Actors     map[string]string `json:"actors"` // display name -> normalized
}

// ParseBDExport reads bd export JSONL. Blank lines and # comments are skipped.
func ParseBDExport(r io.Reader) ([]BDRecord, []string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	var recs []BDRecord
	var warnings []string
	line := 0
	for sc.Scan() {
		line++
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		var rec BDRecord
		if err := json.Unmarshal([]byte(t), &rec); err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		recs = append(recs, rec)
	}
	return recs, warnings, sc.Err()
}

// ImportBD builds the one-commit import of a bd export into project. The
// report is filled during Apply (also on dry runs, which write nothing).
func ImportBD(env Env, recs []BDRecord, dryRun, force bool) (gitops.Operation, *ImportReport) {
	rep := &ImportReport{Actors: map[string]string{}}
	nIssues, nMemories := 0, 0
	for _, r := range recs {
		if r.Type == "memory" {
			nMemories++
		} else {
			nIssues++
		}
	}
	subject := fmt.Sprintf("(%d issues, %d memories)", nIssues, nMemories)
	return gitops.Operation{Verb: "import bd", ID: subject, Summary: env.Project, Apply: func(hubDir string) ([]string, error) {
		*rep = ImportReport{Actors: map[string]string{}}
		wf := env.workflow(env.Project)
		projectDir := filepath.Join("projects", env.Project)

		// First pass: plan every file.
		type planned struct {
			rel string
			iss *issue.Issue
		}
		var plans []planned
		var memories []planned
		basenameByID := map[string]string{}
		for _, r := range recs {
			if r.Type == "memory" {
				key := strings.TrimSpace(r.Key)
				if !issue.ValidMemoryKey(key) {
					rep.Rejected = append(rep.Rejected, fmt.Sprintf("memory %q: invalid key", r.Key))
					continue
				}
				rel := filepath.ToSlash(filepath.Join(projectDir, "memories", key+".md"))
				memories = append(memories, planned{rel: rel})
				m := &issue.Memory{Key: key, Body: strings.TrimRight(r.Value, "\n") + "\n"}
				data, _ := issue.EncodeMemory(m)
				memories[len(memories)-1].iss = &issue.Issue{Body: string(data)} // carrier for bytes
				continue
			}
			if r.ID == "" || strings.TrimSpace(r.Title) == "" {
				rep.Rejected = append(rep.Rejected, fmt.Sprintf("%q: missing id or title", r.ID))
				continue
			}
			if !issue.ValidID(r.ID) {
				rep.Rejected = append(rep.Rejected, fmt.Sprintf("%s: id does not match the grammar", r.ID))
				continue
			}
			if !wf.IsValid(r.Status) {
				rep.Rejected = append(rep.Rejected, fmt.Sprintf("%s: status %q is not in the workflow", r.ID, r.Status))
				continue
			}
			typ := r.IssueType
			if typ == "" || !env.Types.ValidType(typ) {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: type %q becomes task", r.ID, r.IssueType))
				typ = "task"
			}
			created := parseBDTime(r.CreatedAt)
			updated := parseBDTime(r.UpdatedAt)
			if created.IsZero() {
				created = env.now()
			}
			if updated.IsZero() {
				updated = created
			}
			iss := &issue.Issue{ID: r.ID, Title: r.Title, Type: typ, Status: r.Status, Priority: clampPriority(r.Priority),
				Labels: cleanList(r.Labels), URL: r.URL, Created: created, Updated: updated}
			if r.Assignee != "" {
				iss.Assignee = actor(rep, r.Assignee)
			}
			var desc strings.Builder
			if d := strings.TrimRight(r.Description, "\n"); d != "" {
				desc.WriteString(d + "\n")
			}
			var body strings.Builder
			for _, sec := range []struct{ h, text string }{{"Acceptance", r.Acceptance}, {"Design", r.Design}, {"Notes", r.Notes}} {
				if t := strings.TrimRight(sec.text, "\n"); t != "" {
					body.WriteString("\n## " + sec.h + "\n" + t + "\n")
				}
			}
			iss.Description = desc.String()
			if iss.Description != "" && body.Len() > 0 {
				iss.Description += "\n"
			}
			iss.Body = strings.TrimPrefix(body.String(), "\n")
			creator := r.CreatedBy
			if creator == "" {
				creator = r.Owner
			}
			if creator == "" {
				creator = env.Actor
			}
			issue.AppendLog(iss, issue.LogEntry{At: created, Actor: actor(rep, creator), Event: "created"})
			if r.BranchName != "" {
				issue.AppendLog(iss, issue.LogEntry{At: created, Actor: actor(rep, creator), Event: "note — branch " + r.BranchName})
			}
			for _, d := range r.Dependencies {
				if d.IssueID != r.ID || d.DependsOn == "" {
					continue
				}
				switch d.Type {
				case "blocks":
					iss.BlockedBy = append(iss.BlockedBy, issue.NewLink(d.DependsOn))
					rep.Blocks++
				case "parent-child":
					iss.Parent = issue.NewLink(d.DependsOn)
					rep.Parents++
				}
			}
			sub := "issues"
			if wf.IsTerminal(r.Status) {
				closedAt := parseBDTime(r.ClosedAt)
				if closedAt.IsZero() {
					closedAt = updated
				}
				reason := strings.TrimSpace(r.CloseReason)
				if reason == "" {
					reason = "closed in bd"
				}
				issue.AppendLog(iss, issue.LogEntry{At: closedAt, Actor: actor(rep, creator), Event: "closed — " + reason})
				sub = filepath.Join("archive", closedAt.UTC().Format("2006"))
				rep.Archived++
			}
			iss.Updated = updated
			base := strings.TrimSuffix(issue.Filename(r.ID, issue.Slug(r.Title)), ".md")
			basenameByID[r.ID] = base
			plans = append(plans, planned{rel: filepath.ToSlash(filepath.Join(projectDir, sub, base+".md")), iss: iss})
		}

		// Second pass: resolve links to basenames.
		for _, p := range plans {
			if !p.iss.Parent.IsZero() {
				if b, ok := basenameByID[p.iss.Parent.Target]; ok {
					p.iss.Parent = issue.NewLink(b)
				} else {
					rep.Unresolved = append(rep.Unresolved, p.iss.ID+" parent "+p.iss.Parent.Target)
				}
			}
			for i, l := range p.iss.BlockedBy {
				if b, ok := basenameByID[l.Target]; ok {
					p.iss.BlockedBy[i] = issue.NewLink(b)
				} else {
					rep.Unresolved = append(rep.Unresolved, p.iss.ID+" blocked_by "+l.Target)
				}
			}
		}
		rep.Issues = len(plans)
		rep.Memories = len(memories)
		sort.Strings(rep.Rejected)
		sort.Strings(rep.Unresolved)
		if len(rep.Rejected) > 0 {
			return nil, fmt.Errorf("%d record(s) rejected; fix the export or the workflow vocabulary: %s", len(rep.Rejected), strings.Join(rep.Rejected, "; "))
		}
		if dryRun {
			return nil, nil
		}
		if !force {
			for _, p := range append(plans, memories...) {
				if _, err := os.Stat(filepath.Join(hubDir, filepath.FromSlash(p.rel))); err == nil {
					return nil, fmt.Errorf("%s already exists; pass --force to overwrite imported files", p.rel)
				}
			}
		}
		paths, err := ensureProject(hubDir, env.Project, "")
		if err != nil {
			return nil, err
		}
		for _, p := range plans {
			data, err := issue.Encode(p.iss)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p.iss.ID, err)
			}
			if err := gitops.WriteFile(filepath.Join(hubDir, filepath.FromSlash(p.rel)), data); err != nil {
				return nil, err
			}
			paths = append(paths, p.rel)
		}
		for _, m := range memories {
			if err := gitops.WriteFile(filepath.Join(hubDir, filepath.FromSlash(m.rel)), []byte(m.iss.Body)); err != nil {
				return nil, err
			}
			paths = append(paths, m.rel)
		}
		return paths, nil
	}}, rep
}

func actor(rep *ImportReport, display string) string {
	display = strings.TrimSpace(display)
	norm := issue.Slug(display)
	if norm == "" {
		norm = "unknown"
	}
	rep.Actors[display] = norm
	return norm
}

func clampPriority(p int) int {
	if p < 0 {
		return 0
	}
	if p > 4 {
		return 4
	}
	return p
}

func parseBDTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Truncate(time.Second)
		}
	}
	return time.Time{}
}

// ErrDryRun marks a dry run that wrote nothing.
var ErrDryRun = errors.New("dry run")
