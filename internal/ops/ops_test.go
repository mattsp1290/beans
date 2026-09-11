package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/plan"
)

func testEnv(t *testing.T) (Env, string) {
	t.Helper()
	hub := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hub, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	env := Env{HubDir: hub, Project: "p", Actor: "tester", Repo: "p", SHA: "abc1234", Branch: "main",
		Now: func() time.Time { return now }, Types: issue.DefaultTypesConfig(), IDLength: 4}
	return env, hub
}

func apply(t *testing.T, hub string, op gitops.Operation) []string {
	t.Helper()
	paths, err := op.Apply(hub)
	if err != nil {
		t.Fatalf("%s %s: %v", op.Verb, op.ID, err)
	}
	return paths
}

func create(t *testing.T, env Env, hub, title string, in CreateInput) string {
	t.Helper()
	in.Title = title
	op, res := Create(env, in, "p")
	apply(t, hub, op)
	return res.ID
}

func TestCreateWritesFileAndIsReplaySafe(t *testing.T) {
	env, hub := testEnv(t)
	op, res := Create(env, CreateInput{Title: "First: issue", Description: "desc", Labels: []string{"a", "a", "b"}, Priority: 1}, "p")
	paths := apply(t, hub, op)
	if len(paths) != 7 || !strings.HasPrefix(res.ID, "p-") || !strings.HasSuffix(paths[6], res.ID+"-first-issue.md") {
		t.Fatalf("paths=%v id=%s", paths, res.ID)
	}
	iss, _, err := Load(hub, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if iss.Status != "open" || iss.Type != "task" || len(iss.Labels) != 2 || iss.Description != "desc\n\n" || !strings.HasPrefix(iss.Body, "## Acceptance") || len(iss.Log) != 1 || iss.Log[0].Event != "created" || iss.Log[0].SHA != "abc1234" {
		t.Fatalf("issue = %+v", iss)
	}
	again := apply(t, hub, op)
	if len(again) != 0 {
		t.Fatalf("replay must not rewrite: %v", again)
	}
	if _, r := Create(env, CreateInput{Type: "story"}, "p"); r != nil {
		op2, _ := Create(env, CreateInput{Title: "x", Type: "story"}, "p")
		if _, err := op2.Apply(hub); err == nil {
			t.Fatal("unknown type must be rejected")
		}
	}
}

func TestPlanPutRejectsDuplicateIDInAnotherProject(t *testing.T) {
	env, hub := testEnv(t)
	id := "p-plan-a3f2"
	other := filepath.Join(hub, "projects", "other", "plans", "p-plan-a3f2-existing")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := plan.WriteScaffold(other, id, "other project", env.Now()); err != nil {
		t.Fatal(err)
	}
	draftDir := filepath.Join(t.TempDir(), "draft")
	if err := plan.WriteScaffold(draftDir, id, "target project", env.Now()); err != nil {
		t.Fatal(err)
	}
	draft, err := plan.Load(draftDir)
	if err != nil {
		t.Fatal(err)
	}
	op, _, err := PlanPut(env, PlanPutInput{Snapshot: draft.Snapshot(), Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op.Apply(hub); err == nil || !strings.Contains(err.Error(), "already belongs to project") {
		t.Fatalf("cross-project duplicate must be rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "plan.md")); err != nil {
		t.Fatalf("other project plan was changed: %v", err)
	}
}

func TestUpdateFieldsAndLog(t *testing.T) {
	env, hub := testEnv(t)
	id := create(t, env, hub, "Thing", CreateInput{Priority: 2})
	title := "Renamed"
	prio := 0
	paths := apply(t, hub, Update(env, id, UpdateInput{Claim: true, Title: &title, Priority: &prio, AddLabels: []string{"x"}, Note: "hello"}))
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}
	iss, _, _ := Load(hub, id)
	if iss.Status != "in_progress" || iss.Assignee != "tester" || iss.Title != "Renamed" || iss.Priority != 0 || len(iss.Labels) != 1 {
		t.Fatalf("issue = %+v", iss)
	}
	events := []string{}
	for _, e := range iss.Log {
		events = append(events, e.Event)
	}
	want := []string{"created", "status open → in_progress", "field assignee: (none) → tester", "field title: Thing → Renamed", "field priority: 2 → 0", "field labels: + x", "note — hello"}
	if strings.Join(events, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %q", events)
	}
	if p := apply(t, hub, Update(env, id, UpdateInput{Title: &title})); len(p) != 0 {
		t.Fatal("no-op update must change nothing")
	}
	bad := "nope"
	if _, err := Update(env, id, UpdateInput{Status: &bad}).Apply(hub); err == nil {
		t.Fatal("unknown status must be rejected")
	}
}

func TestCloseReopenIdempotent(t *testing.T) {
	env, hub := testEnv(t)
	id := create(t, env, hub, "Thing", CreateInput{})
	apply(t, hub, Close(env, id, "done"))
	iss, _, _ := Load(hub, id)
	if iss.Status != "closed" || iss.Log[len(iss.Log)-1].Event != "closed — done" {
		t.Fatalf("issue = %+v", iss)
	}
	if p := apply(t, hub, Close(env, id, "again")); len(p) != 0 {
		t.Fatal("second close must be a no-op")
	}
	st := "open"
	if _, err := Update(env, id, UpdateInput{Status: &st}).Apply(hub); err == nil {
		t.Fatal("leaving a terminal status needs --force or reopen")
	}
	apply(t, hub, Reopen(env, id))
	iss, _, _ = Load(hub, id)
	if iss.Status != "open" || !strings.HasPrefix(iss.Log[len(iss.Log)-1].Event, "reopened") {
		t.Fatalf("issue = %+v", iss)
	}
}

func TestDepAddRefusesCycleAndDeleteRefusesBacklinks(t *testing.T) {
	env, hub := testEnv(t)
	a := create(t, env, hub, "A", CreateInput{})
	b := create(t, env, hub, "B", CreateInput{})
	c := create(t, env, hub, "C", CreateInput{})
	apply(t, hub, DepAdd(env, b, a, "blocks")) // b blocked by a
	apply(t, hub, DepAdd(env, c, b, "blocks")) // c blocked by b
	if _, err := DepAdd(env, a, c, "blocks").Apply(hub); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("a blocked by c must be refused: %v", err)
	}
	if p := apply(t, hub, DepAdd(env, b, a, "blocks")); len(p) != 0 {
		t.Fatal("duplicate dep must be a no-op")
	}
	apply(t, hub, DepAdd(env, c, a, "parent-child"))
	iss, _, _ := Load(hub, c)
	if !strings.HasPrefix(iss.Parent.Target, a+"-") || len(iss.BlockedBy) != 1 {
		t.Fatalf("issue = %+v", iss)
	}
	if _, err := Delete(env, a, false).Apply(hub); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("delete with backlinks must refuse: %v", err)
	}
	paths := apply(t, hub, Delete(env, a, true))
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
	if _, err := Find(hub, a); err == nil {
		t.Fatal("a still exists")
	}
	bIss, _, _ := Load(hub, b)
	cIss, _, _ := Load(hub, c)
	if len(bIss.BlockedBy) != 0 || !cIss.Parent.IsZero() {
		t.Fatalf("links not removed: b=%+v c=%+v", bIss.BlockedBy, cIss.Parent)
	}
	apply(t, hub, DepRemove(env, c, b, "blocks"))
	cIss, _, _ = Load(hub, c)
	if len(cIss.BlockedBy) != 0 {
		t.Fatal("dep remove failed")
	}
}

func TestArchiveMovesOldTerminalIssues(t *testing.T) {
	env, hub := testEnv(t)
	old := create(t, env, hub, "Old", CreateInput{})
	apply(t, hub, Close(env, old, "x"))
	later := env
	later.Now = func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	fresh := create(t, later, hub, "Fresh", CreateInput{})
	apply(t, hub, Close(later, fresh, "y"))
	open := create(t, later, hub, "Open", CreateInput{})
	op, res := Archive(later, "p", 30*24*time.Hour, true)
	if p := apply(t, hub, op); len(p) != 0 || len(res.Moved) != 1 || res.Moved[0] != old {
		t.Fatalf("dry run: paths=%v moved=%v", p, res.Moved)
	}
	op, res = Archive(later, "p", 30*24*time.Hour, false)
	paths := apply(t, hub, op)
	if len(paths) != 2 || !strings.Contains(paths[1], "/archive/2026/") || len(res.Moved) != 1 {
		t.Fatalf("paths=%v moved=%v", paths, res.Moved)
	}
	moved, loc, err := Load(hub, old)
	if err != nil || !loc.Archived || moved.Log[len(moved.Log)-1].Event != "archived" {
		t.Fatalf("%v %+v", err, loc)
	}
	if _, loc, _ := Load(hub, fresh); loc.Archived {
		t.Fatal("fresh issue must stay")
	}
	if _, loc, _ := Load(hub, open); loc.Archived {
		t.Fatal("open issue must stay")
	}
	apply(t, hub, Reopen(later, old))
	if _, loc, _ := Load(hub, old); loc.Archived {
		t.Fatal("reopen must move the file back")
	}
}

func TestDepAddAcceptsBasenamesAndRefusesParentCycle(t *testing.T) {
	env, hub := testEnv(t)
	a := create(t, env, hub, "A issue", CreateInput{})
	b := create(t, env, hub, "B issue", CreateInput{})
	apply(t, hub, DepAdd(env, b, a, "blocks"))
	// child given as its basename must still be recognised by the cycle check
	if _, err := DepAdd(env, a+"-a-issue", b, "blocks").Apply(hub); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle via basename must be refused: %v", err)
	}
	apply(t, hub, DepAdd(env, b, a, "parent-child"))
	if _, err := DepAdd(env, a, b, "parent-child").Apply(hub); err == nil || !strings.Contains(err.Error(), "parent cycle") {
		t.Fatalf("parent cycle must be refused: %v", err)
	}
	if _, err := DepAdd(env, a, a, "blocks").Apply(hub); err == nil {
		t.Fatal("self dependency must be refused")
	}
	bIss, _, _ := Load(hub, b)
	if bIss.Log[len(bIss.Log)-2].Event != "blocked_by + "+a {
		t.Fatalf("log should name the canonical id: %+v", bIss.Log)
	}
}
