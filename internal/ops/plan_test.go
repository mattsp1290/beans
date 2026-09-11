package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/beans/plan"
)

func TestPlanPutRemovesDroppedSections(t *testing.T) {
	env, hub := testEnv(t)
	firstPlan, err := plan.Parse("plan.md", plan.Scaffold("p-plan-a3f2", "Plan", env.now()))
	if err != nil {
		t.Fatal(err)
	}
	firstPlan.Sections = []string{"sections/old.md"}
	manifest, err := plan.Encode(firstPlan)
	if err != nil {
		t.Fatal(err)
	}
	first := plan.BundleSnapshot{Files: map[string][]byte{
		"plan.md":         manifest,
		"sections/old.md": []byte("# Old\n"),
	}}
	op, _, err := PlanPut(env, PlanPutInput{Snapshot: first, Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, hub, op)

	secondPlan, err := plan.Parse("plan.md", manifest)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan.Sections = nil
	secondManifest, err := plan.Encode(secondPlan)
	if err != nil {
		t.Fatal(err)
	}
	second := plan.BundleSnapshot{Files: map[string][]byte{"plan.md": secondManifest}}
	op, _, err = PlanPut(env, PlanPutInput{Snapshot: second, Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	paths := apply(t, hub, op)
	if _, err := os.Stat(filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-plan", "sections", "old.md")); !os.IsNotExist(err) {
		t.Fatalf("stale section remains: %v", err)
	}
	wantPaths := []string{
		"projects/p/plans/p-plan-a3f2-plan/sections/old.md",
		"projects/p/plans/p-plan-a3f2-plan/plan.md",
	}
	if strings.Join(paths, "|") != strings.Join(wantPaths, "|") {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	if _, err := plan.Load(filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-plan")); err != nil {
		t.Fatalf("updated bundle is invalid: %v", err)
	}
}

func TestPlanPutRejectsStaleRevision(t *testing.T) {
	env, hub := testEnv(t)
	draft, err := plan.Parse("plan.md", plan.Scaffold("p-plan-a3f2", "Plan", env.now()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := plan.Encode(draft)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := plan.BundleSnapshot{Files: map[string][]byte{"plan.md": data}}
	op, _, err := PlanPut(env, PlanPutInput{Snapshot: snapshot, Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, hub, op)
	// A direct publication advances the hub revision; the unchanged local
	// snapshot must not subsequently replace it.
	published, err := plan.Load(filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-plan"))
	if err != nil {
		t.Fatal(err)
	}
	published.Plan.Updated = published.Plan.Updated.Add(time.Second)
	published.Plan.Title = "Newer plan"
	updated, _ := plan.Encode(published.Plan)
	if err := os.WriteFile(filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-plan", "plan.md"), updated, 0o644); err != nil {
		t.Fatal(err)
	}
	op, _, err = PlanPut(env, PlanPutInput{Snapshot: snapshot, Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op.Apply(hub); err == nil || !strings.Contains(err.Error(), "stale plan") {
		t.Fatalf("stale put error = %v", err)
	}
}
