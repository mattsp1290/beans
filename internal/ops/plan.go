package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/plan"
)

type PlanPutInput struct {
	Snapshot plan.BundleSnapshot
	Prefix   string
}
type PlanPutResult struct {
	ID, Path string
	Status   plan.Status
}

// PlanPut publishes the complete, already-captured bundle in one operation.
// The snapshot is never reread, which keeps retries deterministic.
func PlanPut(env Env, in PlanPutInput) (gitops.Operation, *PlanPutResult, error) {
	b, err := plan.LoadSnapshot("local", in.Snapshot)
	if err != nil {
		return gitops.Operation{}, nil, err
	}
	if !plan.ValidID(in.Prefix, b.Plan.ID) {
		return gitops.Operation{}, nil, fmt.Errorf("plan id %q does not match project prefix %q", b.Plan.ID, in.Prefix)
	}
	res := &PlanPutResult{ID: b.Plan.ID, Status: b.Plan.Status}
	return gitops.Operation{Verb: "plan put", ID: b.Plan.ID, Summary: b.Plan.Title, Apply: func(hubDir string) ([]string, error) {
		found, root, current, err := findPlan(hubDir, b.Plan.ID)
		if err != nil {
			return nil, err
		}
		if found && current.Plan.Status == plan.StatusComplete {
			if !sameSnapshot(current.Snapshot(), in.Snapshot) {
				return nil, fmt.Errorf("completed plan %s is immutable", b.Plan.ID)
			}
			res.Path = filepath.ToSlash(filepath.Join(root, "plan.md"))
			return nil, nil
		}
		if found {
			root = filepath.ToSlash(root)
		} else {
			root = filepath.ToSlash(filepath.Join("projects", env.Project, "plans", plan.DirectoryName(b.Plan.ID, b.Plan.Slug)))
		}
		if found && current.Plan.Slug != b.Plan.Slug {
			return nil, fmt.Errorf("plan slug is immutable")
		}
		if found && current.Plan.Created != b.Plan.Created {
			return nil, fmt.Errorf("plan created timestamp is immutable")
		}
		if found {
			for name := range current.Snapshot().Files {
				if _, keep := in.Snapshot.Files[name]; keep {
					continue
				}
				if err := os.Remove(filepath.Join(hubDir, filepath.FromSlash(root), filepath.FromSlash(name))); err != nil && !os.IsNotExist(err) {
					return nil, err
				}
			}
		}
		for name, data := range in.Snapshot.Files {
			if err := gitops.WriteFile(filepath.Join(hubDir, filepath.FromSlash(root), filepath.FromSlash(name)), data); err != nil {
				return nil, err
			}
		}
		res.Path = filepath.ToSlash(filepath.Join(root, "plan.md"))
		return snapshotPaths(root, in.Snapshot), nil
	}}, res, nil
}
func findPlan(hub, id string) (bool, string, *plan.Bundle, error) {
	var root string
	err := filepath.WalkDir(filepath.Join(hub, "projects"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if !d.IsDir() || d.Name() != "plans" {
			return nil
		}
		entries, _ := os.ReadDir(p)
		for _, x := range entries {
			if !x.IsDir() {
				continue
			}
			b, e := plan.Load(filepath.Join(p, x.Name()))
			if e == nil && b.Plan.ID == id {
				r, _ := filepath.Rel(hub, filepath.Join(p, x.Name()))
				root = filepath.ToSlash(r)
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return false, "", nil, err
	}
	if root == "" {
		return false, "", nil, nil
	}
	b, e := plan.Load(filepath.Join(hub, filepath.FromSlash(root)))
	return true, root, b, e
}
func sameSnapshot(a, b plan.BundleSnapshot) bool {
	if len(a.Files) != len(b.Files) {
		return false
	}
	for k, v := range a.Files {
		if string(v) != string(b.Files[k]) {
			return false
		}
	}
	return true
}
func snapshotPaths(root string, s plan.BundleSnapshot) []string {
	out := make([]string, 0, len(s.Files))
	for p := range s.Files {
		out = append(out, strings.TrimPrefix(filepath.ToSlash(filepath.Join(root, p)), "./"))
	}
	return out
}
