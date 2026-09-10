package ops

import (
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

// DepAdd makes child blocked by parent (kind "blocks") or sets child's
// parent (kind "parent-child"). A blocks edge that would create a cycle is
// refused.
func DepAdd(env Env, child, parent, kind string) gitops.Operation {
	return gitops.Operation{Verb: "dep add", ID: child, Summary: parent, Apply: func(hubDir string) ([]string, error) {
		if child == parent {
			return nil, fmt.Errorf("%s cannot depend on itself", child)
		}
		iss, loc, err := Load(hubDir, child)
		if err != nil {
			return nil, err
		}
		pIss, ploc, err := Load(hubDir, parent)
		if err != nil {
			return nil, err
		}
		// Work with canonical ids from here on: the arguments may have been
		// given as basenames, which Find accepts.
		child, parent = iss.ID, pIss.ID
		if child == parent {
			return nil, fmt.Errorf("%s cannot depend on itself", child)
		}
		target := strings.TrimSuffix(filepath.Base(ploc.Path), ".md")
		switch kind {
		case "", "blocks":
			for _, l := range iss.BlockedBy {
				if l.Target == target || l.Target == parent {
					return nil, nil
				}
			}
			ix, err := vault.Load(hubDir)
			if err != nil {
				return nil, err
			}
			if path := wouldCycle(ix, child, parent); len(path) > 0 {
				return nil, fmt.Errorf("adding %s → %s would create a cycle: %s", child, parent, strings.Join(path, " → "))
			}
			iss.BlockedBy = append(iss.BlockedBy, issue.NewLink(target))
			issue.AppendLog(iss, env.entry("blocked_by + "+parent))
		case "parent-child":
			if iss.Parent.Target == target {
				return nil, nil
			}
			ix, err := vault.Load(hubDir)
			if err != nil {
				return nil, err
			}
			if path := parentChainCycle(ix, child, parent); len(path) > 0 {
				return nil, fmt.Errorf("making %s a child of %s would create a parent cycle: %s", child, parent, strings.Join(path, " → "))
			}
			issue.AppendLog(iss, env.entry("field parent: "+orEmpty(iss.Parent.Target)+" → "+target))
			iss.Parent = issue.NewLink(target)
		default:
			return nil, fmt.Errorf("unknown dependency type %q (blocks or parent-child)", kind)
		}
		iss.Updated = env.now()
		if err := save(hubDir, loc, iss); err != nil {
			return nil, err
		}
		return []string{loc.Rel}, nil
	}}
}

// DepRemove is the inverse of DepAdd.
func DepRemove(env Env, child, parent, kind string) gitops.Operation {
	return gitops.Operation{Verb: "dep remove", ID: child, Summary: parent, Apply: func(hubDir string) ([]string, error) {
		iss, loc, err := Load(hubDir, child)
		if err != nil {
			return nil, err
		}
		target := parent
		if pIss, ploc, err := Load(hubDir, parent); err == nil {
			target = strings.TrimSuffix(filepath.Base(ploc.Path), ".md")
			parent = pIss.ID
		}
		switch kind {
		case "", "blocks":
			var kept []issue.Link
			for _, l := range iss.BlockedBy {
				if l.Target != target && l.Target != parent {
					kept = append(kept, l)
				}
			}
			if len(kept) == len(iss.BlockedBy) {
				return nil, nil
			}
			iss.BlockedBy = kept
			issue.AppendLog(iss, env.entry("blocked_by - "+parent))
		case "parent-child":
			if iss.Parent.Target != target && iss.Parent.Target != parent {
				return nil, nil
			}
			issue.AppendLog(iss, env.entry("field parent: "+iss.Parent.Target+" → (none)"))
			iss.Parent = issue.Link{}
		default:
			return nil, fmt.Errorf("unknown dependency type %q (blocks or parent-child)", kind)
		}
		iss.Updated = env.now()
		if err := save(hubDir, loc, iss); err != nil {
			return nil, err
		}
		return []string{loc.Rel}, nil
	}}
}

// ArchiveResult lists what Archive moved.
type ArchiveResult struct {
	Moved []string // ids
}

// Archive moves every terminal, non-archived issue (in project, or all)
// whose last log entry is older than olderThan into archive/<YYYY>/.
func Archive(env Env, project string, olderThan time.Duration, dryRun bool) (gitops.Operation, *ArchiveResult) {
	res := &ArchiveResult{}
	return gitops.Operation{Verb: "archive", ID: project, Summary: fmt.Sprintf("older than %s", olderThan), Apply: func(hubDir string) ([]string, error) {
		ix, err := vault.Load(hubDir)
		if err != nil {
			return nil, err
		}
		cutoff := env.now().Add(-olderThan)
		var paths []string
		res.Moved = nil
		ids := make([]string, 0, len(ix.Issues))
		for id := range ix.Issues {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			iss := ix.Issues[id]
			if iss.Archived || (project != "" && iss.Project != project) {
				continue
			}
			if !ix.WorkflowFor(iss.Project).IsTerminal(iss.Status) {
				continue
			}
			last := iss.Updated
			if n := len(iss.Log); n > 0 && !iss.Log[n-1].At.IsZero() {
				last = iss.Log[n-1].At
			}
			if last.After(cutoff) {
				continue
			}
			res.Moved = append(res.Moved, id)
			if dryRun {
				continue
			}
			cur, loc, err := Load(hubDir, id)
			if err != nil {
				return nil, err
			}
			year := last.UTC().Format("2006")
			newRel := filepath.ToSlash(filepath.Join("projects", loc.Project, "archive", year, filepath.Base(loc.Path)))
			issue.AppendLog(cur, env.entry("archived"))
			if err := os.Remove(loc.Path); err != nil {
				return nil, err
			}
			paths = append(paths, loc.Rel)
			cur.Archived = true
			if err := save(hubDir, Located{Rel: newRel}, cur); err != nil {
				return nil, err
			}
			paths = append(paths, newRel)
		}
		if dryRun {
			return nil, nil
		}
		return paths, nil
	}}, res
}

// wouldCycle reports the path parent → … → child through blocked_by edges
// when child blocked-by parent would close a cycle; nil otherwise.
func wouldCycle(ix *vault.Index, child, parent string) []string {
	visited := map[string]bool{}
	var path []string
	var walk func(id string) bool
	walk = func(id string) bool {
		if id == child {
			path = append(path, id)
			return true
		}
		if visited[id] {
			return false
		}
		visited[id] = true
		iss, ok := ix.Issues[id]
		if !ok {
			return false
		}
		path = append(path, id)
		for _, l := range iss.BlockedBy {
			n, ok := ix.Lookup(l.Target)
			if !ok || n.Issue == nil {
				continue
			}
			if walk(n.Issue.ID) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if walk(parent) {
		return append([]string{child}, path...)
	}
	return nil
}

// parentChainCycle reports the parent chain parent → … → child when setting
// child's parent to parent would close a cycle; nil otherwise.
func parentChainCycle(ix *vault.Index, child, parent string) []string {
	var path []string
	visited := map[string]bool{}
	cur := parent
	for cur != "" && !visited[cur] {
		visited[cur] = true
		path = append(path, cur)
		if cur == child {
			return append([]string{child}, path...)
		}
		iss, ok := ix.Issues[cur]
		if !ok || iss.Parent.IsZero() {
			return nil
		}
		n, ok := ix.Lookup(iss.Parent.Target)
		if !ok || n.Issue == nil {
			return nil
		}
		cur = n.Issue.ID
	}
	return nil
}
