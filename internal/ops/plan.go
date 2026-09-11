package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/plan"
	"github.com/mattsp1290/beans/vault"
)

type PlanPutInput struct {
	Snapshot plan.BundleSnapshot
	Prefix   string
}
type PlanPutResult struct {
	ID, Path string
	Status   plan.Status
	Updated  time.Time
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
	// A first publication retains the authored revision. Replacement
	// publication receives a strictly newer optimistic revision.
	if existing, _, _, lookupErr := findPlan(env.HubDir, env.Project, b.Plan.ID); lookupErr == nil && existing {
		if !operationTime.After(b.Plan.Updated) {
			operationTime = b.Plan.Updated.Add(time.Second)
		}
		desiredPlan.Updated = operationTime
	}
	desired := (&plan.Bundle{Plan: &desiredPlan, Sections: b.Sections}).Snapshot()
	return gitops.Operation{Verb: "plan put", ID: b.Plan.ID, Summary: b.Plan.Title, RequireFreshBase: true, Apply: func(hubDir string) ([]string, error) {
		found, root, current, err := findPlan(hubDir, env.Project, b.Plan.ID)
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
		if found && current.Plan.Updated.Equal(desiredPlan.Updated) && sameIgnoringUpdated(current, &plan.Bundle{Plan: &desiredPlan, Sections: b.Sections}) {
			res.Path = filepath.ToSlash(filepath.Join(root, "plan.md"))
			res.Updated = current.Plan.Updated
			return nil, nil
		}
		if found && !current.Plan.Updated.Equal(b.Plan.Updated) {
			return nil, fmt.Errorf("stale plan %s: expected updated %s, current %s; run bn plan get and merge changes", b.Plan.ID, b.Plan.Updated.Format(time.RFC3339), current.Plan.Updated.Format(time.RFC3339))
		}
		if found && sameIgnoringUpdated(current, b) {
			res.Path = filepath.ToSlash(filepath.Join(root, "plan.md"))
			res.Updated = current.Plan.Updated
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
		removed := []string(nil)
		if found {
			for _, name := range current.Snapshot().Paths() {
				if _, retained := desired.Files[name]; !retained {
					removed = append(removed, filepath.ToSlash(filepath.Join(root, name)))
				}
			}
		}
		if err := gitops.ReplaceTree(filepath.Join(hubDir, filepath.FromSlash(root)), desired.Files); err != nil {
			return nil, err
		}
		res.Path = filepath.ToSlash(filepath.Join(root, "plan.md"))
		res.Updated = desiredPlan.Updated
		return append(removed, snapshotPaths(root, desired)...), nil
	}}, res, nil
}

type PlanLinkResult struct {
	PlanID, NodeID, IssueID, Ref, FormerRef string
	Updated                                 time.Time
}
type PlanUnlinkResult = PlanLinkResult

// PlanLink changes one published graph reference. It reloads both plan and
// issue index on every Apply so a push-race replay preserves other nodes.
func PlanLink(env Env, planID, nodeID, issueID string, force bool) (gitops.Operation, *PlanLinkResult) {
	res := &PlanLinkResult{PlanID: planID, NodeID: nodeID}
	at := env.now()
	return gitops.Operation{Verb: "plan link", ID: planID, Summary: nodeID, Apply: func(hub string) ([]string, error) {
		found, root, b, err := findPlan(hub, env.Project, planID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("plan %s not found", planID)
		}
		if b.Plan.Status == plan.StatusComplete {
			return nil, fmt.Errorf("completed plan %s is immutable", planID)
		}
		ix, err := vault.Load(hub)
		if err != nil {
			return nil, err
		}
		_, iss, ok := ix.ResolveIssueRef(issueID)
		if !ok {
			return nil, fmt.Errorf("issue %s not found", issueID)
		}
		var current string
		exists := false
		for _, n := range b.Plan.Graph.Nodes {
			if n.ID == nodeID {
				current = n.Ref
				exists = true
				break
			}
		}
		if !exists {
			return nil, fmt.Errorf("plan %s graph node %s not found", planID, nodeID)
		}
		_, curIssue, curOK := ix.ResolveIssueRef(current)
		if curOK && curIssue.ID == iss.ID {
			res.IssueID = iss.ID
			res.Ref = current
			res.Updated = b.Plan.Updated
			return nil, nil
		}
		if current != "" && !force {
			return nil, fmt.Errorf("plan %s node %s already has ref %q (use --force to replace)", planID, nodeID, current)
		}
		res.FormerRef = current
		if err := plan.SetNodeRef(b.Plan, nodeID, iss.ID); err != nil {
			return nil, err
		}
		if !at.After(b.Plan.Updated) {
			b.Plan.Updated = b.Plan.Updated.Add(time.Second)
		} else {
			b.Plan.Updated = at
		}
		data, err := plan.Encode(b.Plan)
		if err != nil {
			return nil, err
		}
		if err = gitops.WritePlanFile(filepath.Join(hub, filepath.FromSlash(root), "plan.md"), data); err != nil {
			return nil, err
		}
		res.IssueID = iss.ID
		res.Ref = iss.ID
		res.Updated = b.Plan.Updated
		return []string{filepath.ToSlash(filepath.Join(root, "plan.md"))}, nil
	}}, res
}
func PlanUnlink(env Env, planID, nodeID, expected string) (gitops.Operation, *PlanUnlinkResult) {
	res := &PlanLinkResult{PlanID: planID, NodeID: nodeID}
	at := env.now()
	return gitops.Operation{Verb: "plan unlink", ID: planID, Summary: nodeID, Apply: func(hub string) ([]string, error) {
		found, root, b, err := findPlan(hub, env.Project, planID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("plan %s not found", planID)
		}
		if b.Plan.Status == plan.StatusComplete {
			return nil, fmt.Errorf("completed plan %s is immutable", planID)
		}
		ix, err := vault.Load(hub)
		if err != nil {
			return nil, err
		}
		var current string
		exists := false
		for _, n := range b.Plan.Graph.Nodes {
			if n.ID == nodeID {
				current = n.Ref
				exists = true
				break
			}
		}
		if !exists {
			return nil, fmt.Errorf("plan %s graph node %s not found", planID, nodeID)
		}
		expectedTarget := issue.ParseLink(expected).Target
		_, expectedIssue, expectedResolved := ix.ResolveIssueRef(expected)
		canonicalExpected := expectedTarget
		if expectedResolved {
			canonicalExpected = expectedIssue.ID
		}
		if current == "" {
			res.IssueID = canonicalExpected
			res.Ref = ""
			res.Updated = b.Plan.Updated
			return nil, nil
		}
		currentTarget := issue.ParseLink(current).Target
		_, ci, cok := ix.ResolveIssueRef(current)
		ei, eok := expectedIssue, expectedResolved
		match := (cok && eok && ci.ID == ei.ID) || (!eok && issue.ValidID(expectedTarget) && currentTarget == expectedTarget)
		if !match {
			return nil, fmt.Errorf("plan %s node %s ref %q does not match expected issue %q", planID, nodeID, current, expected)
		}
		res.FormerRef = current
		if err := plan.SetNodeRef(b.Plan, nodeID, ""); err != nil {
			return nil, err
		}
		if !at.After(b.Plan.Updated) {
			b.Plan.Updated = b.Plan.Updated.Add(time.Second)
		} else {
			b.Plan.Updated = at
		}
		data, err := plan.Encode(b.Plan)
		if err != nil {
			return nil, err
		}
		if err = gitops.WritePlanFile(filepath.Join(hub, filepath.FromSlash(root), "plan.md"), data); err != nil {
			return nil, err
		}
		if eok {
			res.IssueID = ei.ID
		} else {
			res.IssueID = expectedTarget
		}
		res.Ref = ""
		res.Updated = b.Plan.Updated
		return []string{filepath.ToSlash(filepath.Join(root, "plan.md"))}, nil
	}}, res
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
func findPlan(hub, project, id string) (bool, string, *plan.Bundle, error) {
	var root string
	plansDir := filepath.Join(hub, "projects", project, "plans")
	entries, err := os.ReadDir(plansDir)
	if os.IsNotExist(err) {
		return false, "", nil, rejectPlanIDOutsideProject(hub, project, id)
	}
	if err != nil {
		return false, "", nil, err
	}
	for _, x := range entries {
		if !x.IsDir() {
			continue
		}
		candidate := filepath.Join(plansDir, x.Name())
		if err := gitops.RecoverPlanTemp(filepath.Join(candidate, "plan.md")); err != nil {
			return false, "", nil, err
		}
		if _, statErr := os.Stat(filepath.Join(candidate, "plan.md")); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return false, "", nil, statErr
		}
		b, loadErr := plan.Load(candidate)
		if loadErr != nil {
			return false, "", nil, loadErr
		}
		if b.Plan.ID == id {
			r, _ := filepath.Rel(hub, candidate)
			root = filepath.ToSlash(r)
			break
		}
	}
	if err := rejectPlanIDOutsideProject(hub, project, id); err != nil {
		return false, "", nil, err
	}
	if root == "" {
		return false, "", nil, nil
	}
	b, err := plan.Load(filepath.Join(hub, filepath.FromSlash(root)))
	return true, root, b, err
}

func rejectPlanIDOutsideProject(hub, project, id string) error {
	return filepath.WalkDir(filepath.Join(hub, "projects"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() || d.Name() != "plans" || filepath.Base(filepath.Dir(p)) == project {
			return nil
		}
		entries, readErr := os.ReadDir(p)
		if readErr != nil {
			return readErr
		}
		for _, x := range entries {
			if !x.IsDir() {
				continue
			}
			candidate := filepath.Join(p, x.Name())
			if _, statErr := os.Stat(filepath.Join(candidate, "plan.md")); statErr != nil {
				if os.IsNotExist(statErr) {
					continue
				}
				return statErr
			}
			b, loadErr := plan.Load(candidate)
			if loadErr != nil {
				return loadErr
			}
			if b.Plan.ID == id {
				return fmt.Errorf("plan id %q already belongs to project %q", id, filepath.Base(filepath.Dir(p)))
			}
		}
		return nil
	})
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
