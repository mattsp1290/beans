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

type HandoffCreateInput struct{ Title, Body, Issue, Remote string }
type HandoffCreateResult struct{ ID, Path string }
type HandoffArchiveInput struct {
	IDs       []string
	OlderThan time.Duration
	Project   string
	DryRun    bool
}
type HandoffArchiveResult struct {
	Moved, AlreadyArchived, AlreadyLive []string
}
type handoffLoc struct {
	Path, Rel, Project string
	Archived           bool
}

func findHandoff(hub, id string) (handoffLoc, error) {
	ix, e := vault.Load(hub)
	if e != nil {
		return handoffLoc{}, e
	}
	h, ok := ix.HandoffByID(id)
	if !ok {
		return handoffLoc{}, fmt.Errorf("%w: handoff %s", ErrNotFound, id)
	}
	return handoffLoc{Path: filepath.Join(hub, filepath.FromSlash(h.Path)), Rel: h.Path, Project: h.Project, Archived: h.Archived}, nil
}
func loadHandoff(hub, id string) (*issue.Handoff, handoffLoc, error) {
	l, e := findHandoff(hub, id)
	if e != nil {
		return nil, l, e
	}
	b, e := os.ReadFile(l.Path)
	if e != nil {
		return nil, l, e
	}
	h, e := issue.ParseHandoff(l.Rel, b)
	return h, l, e
}
func saveHandoff(hub string, l handoffLoc, h *issue.Handoff) error {
	b, e := issue.EncodeHandoff(h)
	if e != nil {
		return e
	}
	return gitops.WriteFile(filepath.Join(hub, filepath.FromSlash(l.Rel)), b)
}
func handoffExists(hub, id string) bool { _, e := findHandoff(hub, id); return e == nil }

func HandoffCreate(env Env, in HandoffCreateInput, prefix string) (gitops.Operation, *HandoffCreateResult) {
	now := env.now()
	body := in.Body
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	res := &HandoffCreateResult{ID: issue.NewID(prefix, func(id string) bool { return existsID(env.HubDir)(id) || handoffExists(env.HubDir, id) }, env.IDLength)}
	return gitops.Operation{Verb: "handoff create", ID: res.ID, Summary: in.Title, Apply: func(hub string) ([]string, error) {
		if strings.TrimSpace(in.Title) == "" {
			return nil, errors.New("title is required")
		}
		if strings.TrimSpace(body) == "" {
			return nil, errors.New("handoff body is required")
		}
		expectedIssue := ""
		if in.Issue != "" {
			_, l, err := Load(hub, in.Issue)
			if err != nil {
				return nil, err
			}
			expectedIssue = strings.TrimSuffix(filepath.Base(l.Rel), ".md")
		}
		paths, e := ensureProject(hub, env.Project, in.Remote)
		if e != nil {
			return nil, e
		}
		for _, sub := range []string{"handoffs", "handoffs/archive"} {
			keep := filepath.Join(hub, "projects", env.Project, sub, ".gitkeep")
			if _, err := os.Stat(keep); errors.Is(err, os.ErrNotExist) {
				if err := gitops.WriteFile(keep, nil); err != nil {
					return nil, err
				}
				paths = append(paths, filepath.ToSlash(filepath.Join("projects", env.Project, sub, ".gitkeep")))
			}
		}
		for {
			res.Path = filepath.ToSlash(filepath.Join("projects", env.Project, "handoffs", issue.Filename(res.ID, issue.Slug(in.Title))))
			dst := filepath.Join(hub, filepath.FromSlash(res.Path))
			if b, x := os.ReadFile(dst); x == nil {
				h, x := issue.ParseHandoff(res.Path, b)
				if x == nil && h.ID == res.ID && h.Title == in.Title && h.Body == body && h.Issue.Target == expectedIssue && h.Created.Equal(now) && h.Updated.Equal(now) {
					return append(paths, res.Path), nil
				}
				return nil, fmt.Errorf("handoff destination %s already exists with different content", res.Path)
			}
			if _, x := Find(hub, res.ID); x == nil || handoffExists(hub, res.ID) {
				res.ID = issue.NewID(prefix, func(id string) bool { return existsID(hub)(id) || handoffExists(hub, id) }, env.IDLength)
				continue
			}
			break
		}
		h := &issue.Handoff{ID: res.ID, Aliases: []string{res.ID}, Title: in.Title, Created: now, Updated: now, Body: body}
		if expectedIssue != "" {
			h.Issue = issue.NewLink(expectedIssue)
		}
		if e := saveHandoff(hub, handoffLoc{Rel: res.Path}, h); e != nil {
			return nil, e
		}
		return append(paths, res.Path), nil
	}}, res
}

func HandoffAttach(env Env, id, target string) gitops.Operation {
	return gitops.Operation{Verb: "handoff attach", ID: id, Apply: func(hub string) ([]string, error) {
		h, l, e := loadHandoff(hub, id)
		if e != nil {
			return nil, e
		}
		if l.Archived {
			return nil, errors.New("restore archived handoff before attaching")
		}
		iss, il, e := Load(hub, target)
		if e != nil {
			return nil, e
		}
		_ = iss
		link := issue.NewLink(strings.TrimSuffix(filepath.Base(il.Rel), ".md"))
		if h.Issue.Target == link.Target {
			return nil, nil
		}
		h.Issue = link
		h.Updated = env.now()
		e = saveHandoff(hub, l, h)
		return []string{l.Rel}, e
	}}
}
func HandoffDetach(env Env, id string) gitops.Operation {
	return gitops.Operation{Verb: "handoff detach", ID: id, Apply: func(hub string) ([]string, error) {
		h, l, e := loadHandoff(hub, id)
		if e != nil {
			return nil, e
		}
		if l.Archived {
			return nil, errors.New("restore archived handoff before detaching")
		}
		if h.Issue.IsZero() {
			return nil, nil
		}
		h.Issue = issue.Link{}
		h.Updated = env.now()
		e = saveHandoff(hub, l, h)
		return []string{l.Rel}, e
	}}
}

func HandoffArchive(env Env, in HandoffArchiveInput) (gitops.Operation, *HandoffArchiveResult) {
	res := &HandoffArchiveResult{}
	cutoff := env.now().Add(-in.OlderThan)
	return gitops.Operation{Verb: "handoff archive", ID: "handoff", Apply: func(hub string) ([]string, error) {
		res.Moved, res.AlreadyArchived, res.AlreadyLive = nil, nil, nil
		ix, e := vault.Load(hub)
		if e != nil {
			return nil, e
		}
		ids := append([]string(nil), in.IDs...)
		if len(ids) == 0 {
			for _, h := range ix.ProjectHandoffs(in.Project, false) {
				if !h.Updated.After(cutoff) {
					ids = append(ids, h.ID)
				}
			}
		}
		ids = unique(ids)
		sort.Strings(ids)
		for _, id := range ids {
			if _, ok := ix.HandoffByID(id); !ok {
				return nil, fmt.Errorf("%w: handoff %s", ErrNotFound, id)
			}
		}
		type move struct {
			id     string
			loc    handoffLoc
			dstRel string
			data   []byte
		}
		moves := []move{}
		for _, id := range ids {
			h, l, e := loadHandoff(hub, id)
			if e != nil {
				return nil, e
			}
			if l.Archived {
				res.AlreadyArchived = append(res.AlreadyArchived, id)
				continue
			}
			if in.DryRun {
				res.Moved = append(res.Moved, id)
				continue
			}
			b, e := os.ReadFile(l.Path)
			if e != nil {
				return nil, e
			}
			dstRel := filepath.ToSlash(filepath.Join("projects", l.Project, "handoffs", "archive", fmt.Sprintf("%04d", h.Updated.Year()), filepath.Base(l.Rel)))
			dst := filepath.Join(hub, filepath.FromSlash(dstRel))
			if old, x := os.ReadFile(dst); x == nil && string(old) != string(b) {
				return nil, fmt.Errorf("archive destination %s differs", dstRel)
			}
			moves = append(moves, move{id: id, loc: l, dstRel: dstRel, data: b})
		}
		var paths []string
		for _, m := range moves {
			if e := gitops.WriteFile(filepath.Join(hub, filepath.FromSlash(m.dstRel)), m.data); e != nil {
				return nil, e
			}
			if e := os.Remove(m.loc.Path); e != nil && !errors.Is(e, os.ErrNotExist) {
				return nil, e
			}
			paths = append(paths, m.loc.Rel, m.dstRel)
			res.Moved = append(res.Moved, m.id)
		}
		return paths, nil
	}}, res
}
func HandoffRestore(env Env, ids []string) (gitops.Operation, *HandoffArchiveResult) {
	res := &HandoffArchiveResult{}
	return gitops.Operation{Verb: "handoff restore", ID: "handoff", Apply: func(hub string) ([]string, error) {
		res.Moved, res.AlreadyArchived, res.AlreadyLive = nil, nil, nil
		ids = unique(ids)
		sort.Strings(ids)
		ix, err := vault.Load(hub)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, ok := ix.HandoffByID(id); !ok {
				return nil, fmt.Errorf("%w: handoff %s", ErrNotFound, id)
			}
		}
		type move struct {
			id     string
			loc    handoffLoc
			dstRel string
			data   []byte
		}
		moves := []move{}
		for _, id := range ids {
			_, l, e := loadHandoff(hub, id)
			if e != nil {
				return nil, e
			}
			if !l.Archived {
				res.AlreadyLive = append(res.AlreadyLive, id)
				continue
			}
			b, e := os.ReadFile(l.Path)
			if e != nil {
				return nil, e
			}
			dstRel := filepath.ToSlash(filepath.Join("projects", l.Project, "handoffs", filepath.Base(l.Rel)))
			dst := filepath.Join(hub, filepath.FromSlash(dstRel))
			if old, x := os.ReadFile(dst); x == nil && string(old) != string(b) {
				return nil, fmt.Errorf("restore destination %s differs", dstRel)
			}
			moves = append(moves, move{id: id, loc: l, dstRel: dstRel, data: b})
		}
		var paths []string
		for _, m := range moves {
			if e := gitops.WriteFile(filepath.Join(hub, filepath.FromSlash(m.dstRel)), m.data); e != nil {
				return nil, e
			}
			if e := os.Remove(m.loc.Path); e != nil {
				return nil, e
			}
			paths = append(paths, m.loc.Rel, m.dstRel)
			res.Moved = append(res.Moved, m.id)
		}
		return paths, nil
	}}, res
}
func unique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
