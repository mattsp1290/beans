package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

// ErrRequestNotFound is returned when an id names no request file in the hub.
var ErrRequestNotFound = errors.New("request not found")

// RequestLocated identifies a request file found by its parsed stable id.
type RequestLocated struct{ Path, Rel, Project string }

// FindRequest locates a request by parsing request files and comparing ids.
func FindRequest(hubDir, id string) (RequestLocated, error) {
	projects, err := vault.ProjectDirs(hubDir)
	if err != nil {
		return RequestLocated{}, err
	}
	for _, project := range projects {
		dir := filepath.Join(hubDir, "projects", project, "requests")
		var found RequestLocated
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			rel, _ := filepath.Rel(hubDir, path)
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			r, err := issue.ParseRequest(rel, data)
			if err != nil {
				return err
			}
			if r.ID == id {
				found = RequestLocated{Path: path, Rel: rel, Project: project}
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return RequestLocated{}, err
		}
		if found.Path != "" {
			return found, nil
		}
	}
	return RequestLocated{}, fmt.Errorf("%w: %s", ErrRequestNotFound, id)
}

// LoadRequest parses a located request.
func LoadRequest(hubDir, id string) (*issue.Request, RequestLocated, error) {
	loc, err := FindRequest(hubDir, id)
	if err != nil {
		return nil, loc, err
	}
	data, err := os.ReadFile(loc.Path)
	if err != nil {
		return nil, loc, err
	}
	r, err := issue.ParseRequest(loc.Rel, data)
	return r, loc, err
}

func saveRequest(hubDir string, loc RequestLocated, r *issue.Request) error {
	data, err := issue.EncodeRequest(r)
	if err != nil {
		return err
	}
	return gitops.WriteFile(filepath.Join(hubDir, filepath.FromSlash(loc.Rel)), data)
}

type RequestCreateInput struct {
	Title, Body, RequestedBy, Remote string
	// BodyProvided distinguishes an intentional empty body from no body option,
	// which selects the request template.
	BodyProvided   bool
	Priority       int
	Labels, Issues []string
}
type RequestCreateResult struct{ ID, Path string }

// RequestCreate creates a request through the normal replay-safe operation pipeline.
func RequestCreate(env Env, in RequestCreateInput, prefix string) (gitops.Operation, *RequestCreateResult) {
	res := &RequestCreateResult{ID: issue.NewRequestID(prefix, existsID(env.HubDir), env.IDLength)}
	return gitops.Operation{Verb: "request create", ID: res.ID, Summary: in.Title, Apply: func(hubDir string) ([]string, error) {
		if strings.TrimSpace(in.Title) == "" {
			return nil, errors.New("title is required")
		}
		if in.Priority < 0 || in.Priority > 4 {
			return nil, fmt.Errorf("priority must be 0-4, got %d", in.Priority)
		}
		paths, err := ensureProject(hubDir, env.Project, in.Remote)
		if err != nil {
			return nil, err
		}
		if _, err := FindRequest(hubDir, res.ID); err == nil {
			return paths, nil
		}
		if existsID(hubDir)(res.ID) {
			res.ID = issue.NewRequestID(prefix, existsID(hubDir), env.IDLength)
		}
		issues, err := resolveRequestIssues(hubDir, in.Issues)
		if err != nil {
			return nil, err
		}
		body := in.Body
		if !in.BodyProvided {
			body = issue.LoadRequestTemplate(filepath.Join(hubDir, "projects", env.Project), hubDir)
		}
		body = strings.TrimRight(body, "\n")
		if body != "" {
			body += "\n"
		}
		now := env.now()
		r := &issue.Request{ID: res.ID, Title: in.Title, Status: issue.RequestOpen, Priority: in.Priority, Labels: cleanList(in.Labels), RequestedBy: in.RequestedBy, Issues: issues, Created: now, Updated: now, Body: body}
		issue.AppendRequestLog(r, env.entry("created"))
		res.Path = filepath.ToSlash(filepath.Join("projects", env.Project, "requests", issue.Filename(r.ID, issue.Slug(r.Title))))
		if err := saveRequest(hubDir, RequestLocated{Rel: res.Path}, r); err != nil {
			return nil, err
		}
		return append(paths, res.Path), nil
	}}, res
}

type RequestUpdateInput struct {
	Status, Title, Body, RequestedBy *string
	Priority                         *int
	AddLabels, RemoveLabels          []string
	Force                            bool
}

func (in RequestUpdateInput) IsEmpty() bool {
	return in.Status == nil && in.Title == nil && in.Body == nil && in.Priority == nil && in.RequestedBy == nil && len(in.AddLabels) == 0 && len(in.RemoveLabels) == 0
}

func RequestUpdate(env Env, id string, in RequestUpdateInput) gitops.Operation {
	return gitops.Operation{Verb: "request update", ID: id, Apply: func(hubDir string) ([]string, error) {
		if in.IsEmpty() {
			return nil, errors.New("no request fields supplied")
		}
		r, loc, err := LoadRequest(hubDir, id)
		if err != nil {
			return nil, err
		}
		changed := false
		log := func(event string) { issue.AppendRequestLog(r, env.entry(event)); changed = true }
		if in.Status != nil && *in.Status != r.Status {
			if !issue.ValidRequestStatus(*in.Status) {
				return nil, fmt.Errorf("invalid request status %q", *in.Status)
			}
			if !in.Force {
				if err := issue.ValidateRequestTransition(r.Status, *in.Status); err != nil {
					return nil, err
				}
			}
			from := r.Status
			r.Status = *in.Status
			log(fmt.Sprintf("status %s → %s", from, r.Status))
		}
		if in.Title != nil && *in.Title != r.Title {
			if strings.TrimSpace(*in.Title) == "" {
				return nil, errors.New("title must not be blank")
			}
			log(fmt.Sprintf("field title: %s → %s", r.Title, *in.Title))
			r.Title = *in.Title
		}
		if in.Body != nil && strings.TrimRight(*in.Body, "\n") != strings.TrimRight(r.Body, "\n") {
			issue.SetRequestBody(r, *in.Body)
			log("field body: updated")
		}
		if in.Priority != nil && *in.Priority != r.Priority {
			n := *in.Priority
			if n < 0 || n > 4 {
				return nil, fmt.Errorf("priority must be 0-4, got %d", n)
			}
			log(fmt.Sprintf("field priority: %d → %d", r.Priority, n))
			r.Priority = n
		}
		if in.RequestedBy != nil && *in.RequestedBy != r.RequestedBy {
			log(fmt.Sprintf("field requested_by: %s → %s", orEmpty(r.RequestedBy), orEmpty(*in.RequestedBy)))
			r.RequestedBy = *in.RequestedBy
		}
		for _, label := range cleanList(in.AddLabels) {
			if !contains(r.Labels, label) {
				r.Labels = append(r.Labels, label)
				log("field labels: + " + label)
			}
		}
		for _, label := range cleanList(in.RemoveLabels) {
			if contains(r.Labels, label) {
				r.Labels = remove(r.Labels, label)
				log("field labels: - " + label)
			}
		}
		if !changed {
			return nil, nil
		}
		r.Updated = env.now()
		if err := saveRequest(hubDir, loc, r); err != nil {
			return nil, err
		}
		return []string{loc.Rel}, nil
	}}
}

func resolveRequestIssues(hubDir string, ids []string) ([]issue.Link, error) {
	ix, err := vault.Load(hubDir)
	if err != nil {
		return nil, err
	}
	var out []issue.Link
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		note, ok := ix.Lookup(issue.ParseLink(id).Target)
		if !ok || note.Issue == nil {
			return nil, fmt.Errorf("request issue %q: %w", id, ErrNotFound)
		}
		if seen[note.Issue.ID] {
			continue
		}
		seen[note.Issue.ID] = true
		out = append(out, issue.NewLink(note.Issue.ID))
	}
	return out, nil
}

func RequestLink(env Env, id string, ids []string) gitops.Operation {
	return requestLinkChange(env, id, ids, true)
}
func RequestUnlink(env Env, id string, ids []string) gitops.Operation {
	return requestLinkChange(env, id, ids, false)
}
func requestLinkChange(env Env, id string, ids []string, add bool) gitops.Operation {
	verb := "request unlink"
	if add {
		verb = "request link"
	}
	return gitops.Operation{Verb: verb, ID: id, Apply: func(hubDir string) ([]string, error) {
		if len(ids) == 0 {
			return nil, errors.New("at least one issue id is required")
		}
		r, loc, err := LoadRequest(hubDir, id)
		if err != nil {
			return nil, err
		}
		var targets []string
		if add {
			links, err := resolveRequestIssues(hubDir, ids)
			if err != nil {
				return nil, err
			}
			for _, l := range links {
				targets = append(targets, l.Target)
			}
		} else {
			ix, err := vault.Load(hubDir)
			if err != nil {
				return nil, err
			}
			for _, raw := range cleanList(ids) {
				target := issue.ParseLink(raw).Target
				if n, ok := ix.Lookup(target); ok && n.Issue != nil {
					target = n.Issue.ID
				}
				targets = append(targets, target)
			}
		}
		changed := false
		for _, target := range targets {
			found := false
			for _, l := range r.Issues {
				if l.Target == target {
					found = true
					break
				}
			}
			if add && !found {
				r.Issues = append(r.Issues, issue.NewLink(target))
				issue.AppendRequestLog(r, env.entry("issues + "+target))
				changed = true
			}
			if !add && found {
				var kept []issue.Link
				for _, l := range r.Issues {
					if l.Target != target {
						kept = append(kept, l)
					}
				}
				r.Issues = kept
				issue.AppendRequestLog(r, env.entry("issues - "+target))
				changed = true
			}
		}
		if !changed {
			return nil, nil
		}
		r.Updated = env.now()
		if err := saveRequest(hubDir, loc, r); err != nil {
			return nil, err
		}
		return []string{loc.Rel}, nil
	}}
}
