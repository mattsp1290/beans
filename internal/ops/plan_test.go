package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/plan"
)

func TestPlanPutRemovesDroppedSections(t *testing.T) {
	env, hub := testEnv(t)
	manifest := string(plan.Scaffold("p-plan-a3f2", "Plan", env.now()))
	manifest = strings.Replace(manifest, "updated: 2026-09-10T12:00:00Z\n", "updated: 2026-09-10T12:00:00Z\nsections:\n  - sections/old.md\n", 1)
	first := plan.BundleSnapshot{Files: map[string][]byte{
		"plan.md":         []byte(manifest),
		"sections/old.md": []byte("# Old\n"),
	}}
	op, _, err := PlanPut(env, PlanPutInput{Snapshot: first, Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, hub, op)

	second := plan.BundleSnapshot{Files: map[string][]byte{
		"plan.md": []byte(strings.Replace(manifest, "sections:\n  - sections/old.md\n", "", 1)),
	}}
	op, _, err = PlanPut(env, PlanPutInput{Snapshot: second, Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	paths := apply(t, hub, op)
	if _, err := os.Stat(filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-plan", "sections", "old.md")); !os.IsNotExist(err) {
		t.Fatalf("stale section remains: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want removed section and plan", paths)
	}
	if _, err := plan.Load(filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-plan")); err != nil {
		t.Fatalf("updated bundle is invalid: %v", err)
	}
}
