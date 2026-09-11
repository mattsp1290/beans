package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// Capture this exactly once. Hub.Mutate may replay Apply after a push race.
	operationTime := env.now()
	desiredPlan := *b.Plan
	desiredPlan.Updated = operationTime
	desired := (&plan.Bundle{Plan: &desiredPlan, Sections: b.Sections}).Snapshot()
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
		if found && sameIgnoringUpdated(current, b) {
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
		if err := gitops.ReplaceTree(filepath.Join(hubDir, filepath.FromSlash(root)), desired.Files); err != nil {
			return nil, err
		}
		res.Path = filepath.ToSlash(filepath.Join(root, "plan.md"))
		return snapshotPaths(root, desired), nil
	}}, res, nil
}

func sameIgnoringUpdated(current, source *plan.Bundle) bool {
	if current == nil || source == nil {
		return false
	}
	a, b := *current.Plan, *source.Plan
	a.Updated, b.Updated = time.Time{}, time.Time{}
	if string(mustEncode(&a)) != string(mustEncode(&b)) || len(current.Sections) != len(source.Sections) {
		return false
	}
	for i := range current.Sections {
		if current.Sections[i] != source.Sections[i] {
			return false
		}
	}
	return true
}

func mustEncode(p *plan.Plan) []byte { b, _ := plan.Encode(p); return b }
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
