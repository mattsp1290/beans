// Package ops builds the gitops.Operation values behind every bn mutation.
// The CLI and the HTTP server share them. Every Apply re-reads the files it
// changes so the pipeline can run it again on a different tree.
package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

// Env is what every operation needs to know about the caller.
type Env struct {
	HubDir  string
	Project string // resolved project (target of creates)
	Actor   string
	Repo    string // code repository basename, for log lines
	SHA     string // short head commit
	Branch  string
	Now     func() time.Time
	// Workflow and Types are loaded per project by the caller; WorkflowFor
	// falls back to the hub defaults when a project has no override.
	WorkflowFor func(project string) issue.WorkflowConfig
	Types       issue.TypesConfig
	IDLength    int
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func (e Env) workflow(project string) issue.WorkflowConfig {
	if e.WorkflowFor != nil {
		return e.WorkflowFor(project)
	}
	return issue.DefaultWorkflowConfig()
}

func (e Env) entry(event string) issue.LogEntry {
	return issue.LogEntry{At: e.now(), Actor: e.Actor, Repo: e.Repo, SHA: e.SHA, Branch: e.Branch, Event: event}
}

// ErrNotFound is returned when an id names no issue file in the hub.
var ErrNotFound = errors.New("issue not found")

// Located is an issue file found by id.
type Located struct {
	Path     string // absolute
	Rel      string // hub-relative, slash-separated
	Project  string
	Archived bool
}

// Find locates the file for an issue id anywhere in the hub by filename
// (<id>-<slug>.md or <id>.md under projects/*/issues and archive/**).
func Find(hubDir, id string) (Located, error) {
	projects, err := vault.ProjectDirs(hubDir)
	if err != nil {
		return Located{}, err
	}
	for _, p := range projects {
		base := filepath.Join(hubDir, "projects", p)
		for _, sub := range []string{"issues", "archive"} {
			var found Located
			werr := filepath.WalkDir(filepath.Join(base, sub), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if strings.HasPrefix(d.Name(), ".") && path != filepath.Join(base, sub) {
						return filepath.SkipDir
					}
					return nil
				}
				if matchesID(d.Name(), id) {
					rel, _ := filepath.Rel(hubDir, path)
					found = Located{Path: path, Rel: filepath.ToSlash(rel), Project: p, Archived: sub == "archive"}
					return filepath.SkipAll
				}
				return nil
			})
			if werr != nil {
				return Located{}, werr
			}
			if found.Path != "" {
				return found, nil
			}
		}
	}
	return Located{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func matchesID(name, id string) bool {
	if !strings.HasSuffix(name, ".md") {
		return false
	}
	stem := strings.TrimSuffix(name, ".md")
	return stem == id || strings.HasPrefix(stem, id+"-")
}

// Load parses the issue file for id.
func Load(hubDir, id string) (*issue.Issue, Located, error) {
	loc, err := Find(hubDir, id)
	if err != nil {
		return nil, loc, err
	}
	data, err := os.ReadFile(loc.Path)
	if err != nil {
		return nil, loc, err
	}
	iss, err := issue.Parse(loc.Rel, data)
	if err != nil {
		return nil, loc, err
	}
	iss.Path = loc.Rel
	return iss, loc, nil
}

func save(hubDir string, loc Located, iss *issue.Issue) error {
	data, err := issue.Encode(iss)
	if err != nil {
		return err
	}
	return gitops.WriteFile(filepath.Join(hubDir, filepath.FromSlash(loc.Rel)), data)
}

// existsID reports whether any issue file in the hub carries id.
func existsID(hubDir string) func(string) bool {
	return func(id string) bool {
		_, err := Find(hubDir, id)
		return err == nil
	}
}

// ensureProject makes sure the project directory exists, returning the
// paths it created so they join the operation commit.
func ensureProject(hubDir, project, remote string) ([]string, error) {
	return vault.CreateProjectFiles(hubDir, project, remote)
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

// CreateInput is what bn create and POST /issues accept.
type CreateInput struct {
	Title       string
	Description string
	Priority    int
	Type        string
	Labels      []string
	Parent      string
	Assignee    string
	BlockedBy   []string
	URL         string
	// Remote is recorded when the project is auto-created.
	Remote string
}

// CreateResult is filled in by Apply.
type CreateResult struct {
	ID   string
	Path string
}

// Create builds the create operation. The id is drawn up front so the
// commit subject can name it; on replay Apply re-checks that the id is still
// free (another writer may have raced a creation) and redraws if not.
func Create(env Env, in CreateInput, prefix string) (gitops.Operation, *CreateResult) {
	res := &CreateResult{ID: issue.NewID(prefix, existsID(env.HubDir), env.IDLength)}
	wf := env.workflow(env.Project)
	return gitops.Operation{Verb: "create", ID: res.ID, Summary: in.Title, Apply: func(hubDir string) ([]string, error) {
		if strings.TrimSpace(in.Title) == "" {
			return nil, errors.New("title is required")
		}
		typ := in.Type
		if typ == "" {
			typ = "task"
		}
		if !env.Types.ValidType(typ) {
			return nil, fmt.Errorf("unknown type %q (valid: %s)", typ, strings.Join(env.Types.Names, ", "))
		}
		if in.Priority < 0 || in.Priority > 4 {
			return nil, fmt.Errorf("priority must be 0-4, got %d", in.Priority)
		}
		paths, err := ensureProject(hubDir, env.Project, in.Remote)
		if err != nil {
			return nil, err
		}
		rel := filepath.ToSlash(filepath.Join("projects", env.Project, "issues", issue.Filename(res.ID, issue.Slug(in.Title))))
		res.Path = rel
		if _, err := os.Stat(filepath.Join(hubDir, filepath.FromSlash(rel))); err == nil {
			return paths, nil // replay: already written by this run
		}
		if loc, err := Find(hubDir, res.ID); err == nil {
			// Another writer took this id while we raced; the commit subject
			// keeps the original id text but the file gets a fresh one.
			_ = loc
			res.ID = issue.NewID(prefix, existsID(hubDir), env.IDLength)
			rel = filepath.ToSlash(filepath.Join("projects", env.Project, "issues", issue.Filename(res.ID, issue.Slug(in.Title))))
			res.Path = rel
		}
		now := env.now()
		iss := &issue.Issue{
			ID: res.ID, Title: in.Title, Type: typ, Status: wf.DefaultState(), Priority: in.Priority,
			Labels: cleanList(in.Labels), Assignee: in.Assignee, URL: in.URL, Created: now, Updated: now,
		}
		if in.Parent != "" {
			iss.Parent = issue.NewLink(linkTarget(hubDir, in.Parent))
		}
		for _, b := range in.BlockedBy {
			iss.BlockedBy = append(iss.BlockedBy, issue.NewLink(linkTarget(hubDir, b)))
		}
		desc := strings.TrimRight(in.Description, "\n")
		body := issue.LoadTemplate(typ, filepath.Join(hubDir, "projects", env.Project), hubDir)
		if desc != "" {
			iss.Description = desc + "\n"
			if body != "" {
				iss.Description += "\n"
			}
		}
		iss.Body = body
		issue.AppendLog(iss, env.entry("created"))
		if err := save(hubDir, Located{Rel: rel}, iss); err != nil {
			return nil, err
		}
		return append(paths, rel), nil
	}}, res
}

// linkTarget turns an id into the basename bn writes in a link; when the
// id's file is not found the id itself is used and doctor reports it.
func linkTarget(hubDir, id string) string {
	if loc, err := Find(hubDir, id); err == nil {
		return strings.TrimSuffix(filepath.Base(loc.Path), ".md")
	}
	return id
}

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

// UpdateInput lists optional field changes; nil pointers are untouched.
type UpdateInput struct {
	Claim       bool
	Status      *string
	Title       *string
	Description *string
	Priority    *int
	Type        *string
	Assignee    *string
	AddLabels   []string
	RemoveLabel []string
	Parent      *string // "" clears
	Note        string
	Force       bool // allow leaving a terminal status
}

// IsEmpty reports whether nothing would change.
func (u UpdateInput) IsEmpty() bool {
	return !u.Claim && u.Status == nil && u.Title == nil && u.Description == nil && u.Priority == nil &&
		u.Type == nil && u.Assignee == nil && len(u.AddLabels) == 0 && len(u.RemoveLabel) == 0 && u.Parent == nil && u.Note == ""
}

// Update builds one operation that applies every requested change with one
// log line per changed field.
func Update(env Env, id string, in UpdateInput) gitops.Operation {
	return gitops.Operation{Verb: "update", ID: id, Apply: func(hubDir string) ([]string, error) {
		iss, loc, err := Load(hubDir, id)
		if err != nil {
			return nil, err
		}
		wf := env.workflow(loc.Project)
		changed := false
		logf := func(format string, args ...any) {
			issue.AppendLog(iss, env.entry(fmt.Sprintf(format, args...)))
			changed = true
		}
		setStatus := func(to string) error {
			if to == iss.Status {
				return nil
			}
			if !wf.IsValid(to) {
				return fmt.Errorf("unknown status %q (valid: %s)", to, strings.Join(wf.StatusNames(), ", "))
			}
			if wf.IsTerminal(iss.Status) && !in.Force {
				return fmt.Errorf("%s is %s; use bn reopen or --force to change its status", iss.ID, iss.Status)
			}
			from := iss.Status
			iss.Status = to
			logf("status %s → %s", from, to)
			return nil
		}
		if in.Claim {
			if err := setStatus("in_progress"); err != nil {
				return nil, err
			}
			if iss.Assignee != env.Actor {
				logf("field assignee: %s → %s", orEmpty(iss.Assignee), env.Actor)
				iss.Assignee = env.Actor
			}
		}
		if in.Status != nil {
			if err := setStatus(*in.Status); err != nil {
				return nil, err
			}
		}
		if in.Title != nil && *in.Title != iss.Title {
			if strings.TrimSpace(*in.Title) == "" {
				return nil, errors.New("title must not be empty")
			}
			logf("field title: %s → %s", iss.Title, *in.Title)
			iss.Title = *in.Title
		}
		if in.Description != nil && strings.TrimRight(*in.Description, "\n") != strings.TrimRight(iss.Description, "\n") {
			issue.SetDescription(iss, *in.Description)
			logf("field description: updated")
		}
		if in.Priority != nil && *in.Priority != iss.Priority {
			if *in.Priority < 0 || *in.Priority > 4 {
				return nil, fmt.Errorf("priority must be 0-4, got %d", *in.Priority)
			}
			logf("field priority: %d → %d", iss.Priority, *in.Priority)
			iss.Priority = *in.Priority
		}
		if in.Type != nil && *in.Type != iss.Type {
			if !env.Types.ValidType(*in.Type) {
				return nil, fmt.Errorf("unknown type %q", *in.Type)
			}
			logf("field type: %s → %s", iss.Type, *in.Type)
			iss.Type = *in.Type
		}
		if in.Assignee != nil && *in.Assignee != iss.Assignee {
			logf("field assignee: %s → %s", orEmpty(iss.Assignee), orEmpty(*in.Assignee))
			iss.Assignee = *in.Assignee
		}
		for _, l := range cleanList(in.AddLabels) {
			if !contains(iss.Labels, l) {
				iss.Labels = append(iss.Labels, l)
				logf("field labels: + %s", l)
			}
		}
		for _, l := range cleanList(in.RemoveLabel) {
			if contains(iss.Labels, l) {
				iss.Labels = remove(iss.Labels, l)
				logf("field labels: - %s", l)
			}
		}
		if in.Parent != nil {
			if *in.Parent == "" {
				if !iss.Parent.IsZero() {
					logf("field parent: %s → (none)", iss.Parent.Target)
					iss.Parent = issue.Link{}
				}
			} else {
				target := linkTarget(hubDir, *in.Parent)
				if iss.Parent.Target != target {
					logf("field parent: %s → %s", orEmpty(iss.Parent.Target), target)
					iss.Parent = issue.NewLink(target)
				}
			}
		}
		if strings.TrimSpace(in.Note) != "" {
			logf("note — %s", strings.TrimSpace(in.Note))
		}
		if !changed {
			return nil, nil
		}
		iss.Updated = env.now()
		if err := save(hubDir, loc, iss); err != nil {
			return nil, err
		}
		return []string{loc.Rel}, nil
	}}
}

func orEmpty(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// close / reopen / delete
// ---------------------------------------------------------------------------

// Close moves the issue to the first terminal status and logs the reason.
// Idempotent: an already-terminal issue changes nothing.
func Close(env Env, id, reason string) gitops.Operation {
	return gitops.Operation{Verb: "close", ID: id, Summary: reason, Apply: func(hubDir string) ([]string, error) {
		iss, loc, err := Load(hubDir, id)
		if err != nil {
			return nil, err
		}
		wf := env.workflow(loc.Project)
		if wf.IsTerminal(iss.Status) {
			return nil, nil
		}
		if len(wf.Terminal) == 0 {
			return nil, errors.New("workflow has no terminal status")
		}
		iss.Status = wf.Terminal[0]
		issue.AppendLog(iss, env.entry("closed — "+strings.TrimSpace(reason)))
		iss.Updated = env.now()
		if err := save(hubDir, loc, iss); err != nil {
			return nil, err
		}
		return []string{loc.Rel}, nil
	}}
}

// Reopen returns the issue to the default status and moves it out of the
// archive when needed.
func Reopen(env Env, id string) gitops.Operation {
	return gitops.Operation{Verb: "reopen", ID: id, Apply: func(hubDir string) ([]string, error) {
		iss, loc, err := Load(hubDir, id)
		if err != nil {
			return nil, err
		}
		wf := env.workflow(loc.Project)
		if !wf.IsTerminal(iss.Status) && !loc.Archived {
			return nil, nil
		}
		var paths []string
		if wf.IsTerminal(iss.Status) {
			from := iss.Status
			iss.Status = wf.DefaultState()
			issue.AppendLog(iss, env.entry(fmt.Sprintf("reopened (was %s)", from)))
			iss.Updated = env.now()
		}
		if loc.Archived {
			newRel := filepath.ToSlash(filepath.Join("projects", loc.Project, "issues", filepath.Base(loc.Path)))
			if err := os.Remove(loc.Path); err != nil {
				return nil, err
			}
			paths = append(paths, loc.Rel)
			loc.Rel = newRel
			iss.Archived = false
		}
		if err := save(hubDir, loc, iss); err != nil {
			return nil, err
		}
		return append(paths, loc.Rel), nil
	}}
}

// Delete removes the issue file. Unless force is set it refuses when other
// notes link to it; with force those links are removed in the same commit.
func Delete(env Env, id string, force bool) gitops.Operation {
	return gitops.Operation{Verb: "delete", ID: id, Apply: func(hubDir string) ([]string, error) {
		loc, err := Find(hubDir, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, nil // replay after deletion
			}
			return nil, err
		}
		base := strings.TrimSuffix(filepath.Base(loc.Path), ".md")
		ix, err := vault.Load(hubDir)
		if err != nil {
			return nil, err
		}
		refs := ix.Backlinks[base]
		var paths []string
		if len(refs) > 0 {
			if !force {
				var from []string
				for _, r := range refs {
					from = append(from, r.From)
				}
				sort.Strings(from)
				return nil, fmt.Errorf("%s is referenced by %s; pass --force to delete it and remove those links", id, strings.Join(from, ", "))
			}
			for _, r := range refs {
				if r.Kind != vault.LinkParent && r.Kind != vault.LinkBlockedBy {
					continue
				}
				n, ok := ix.Notes[r.From]
				if !ok || n.Issue == nil {
					continue
				}
				other, oloc, err := Load(hubDir, n.Issue.ID)
				if err != nil {
					return nil, err
				}
				if r.Kind == vault.LinkParent {
					other.Parent = issue.Link{}
					issue.AppendLog(other, env.entry("field parent: "+base+" → (none)"))
				} else {
					var kept []issue.Link
					for _, l := range other.BlockedBy {
						if l.Target != base && l.Target != id {
							kept = append(kept, l)
						}
					}
					other.BlockedBy = kept
					issue.AppendLog(other, env.entry("blocked_by - "+id))
				}
				other.Updated = env.now()
				if err := save(hubDir, oloc, other); err != nil {
					return nil, err
				}
				paths = append(paths, oloc.Rel)
			}
		}
		if err := os.Remove(loc.Path); err != nil {
			return nil, err
		}
		return append(paths, loc.Rel), nil
	}}
}
