package store

import (
	"context"
	"errors"
	"testing"

	"github.com/mattsp1290/beans/model"
)

// TestStoreAcceptsNewHoldStates proves the store validates against the default
// workflow config (which includes the ready_for_* hold states) now that the DB
// CHECK constraint is gone.
func TestStoreAcceptsNewHoldStates(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	ensureContractProject(t, s, ctx, "wf")

	iss := mustCreateContractIssue(t, s, ctx, "wf", "Review me", 2)
	if iss.State != "open" {
		t.Fatalf("new issue state = %q, want open", iss.State)
	}

	for _, st := range []model.IssueState{"ready_for_review", "ready_for_validation", "ready_for_merge"} {
		state := st
		updated, err := s.UpdateIssue(ctx, iss.ID, UpdateIssueInput{State: &state})
		if err != nil {
			t.Fatalf("UpdateIssue to %q: %v", st, err)
		}
		if updated.State != st {
			t.Errorf("state = %q, want %q", updated.State, st)
		}
	}
}

// TestStoreRejectsUnknownStatus is the write-strict half of the invariant: a
// status outside the configured vocabulary is rejected on write.
func TestStoreRejectsUnknownStatus(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	ensureContractProject(t, s, ctx, "wf")
	iss := mustCreateContractIssue(t, s, ctx, "wf", "X", 2)

	bogus := model.IssueState("not_a_real_status")
	if _, err := s.UpdateIssue(ctx, iss.ID, UpdateIssueInput{State: &bogus}); !errors.Is(err, ErrInvalidIssueState) {
		t.Fatalf("UpdateIssue with bogus status = %v, want ErrInvalidIssueState", err)
	}
}

// TestStoreCustomWorkflowDefaultAndVocab proves a deployment-supplied workflow
// config drives both the default-state-on-create and the valid vocabulary.
func TestStoreCustomWorkflowDefaultAndVocab(t *testing.T) {
	ctx := context.Background()
	custom := model.WorkflowConfig{
		Statuses: []model.IssueState{"backlog", "active", "qa", "shipped"},
		Default:  "backlog",
		Active:   []model.IssueState{"active"},
		Terminal: []model.IssueState{"shipped"},
	}
	s, err := New(ctx, Config{
		Driver:   DriverSQLite,
		DSN:      SecretDSN(sqliteMemoryDSN(t)),
		Workflow: custom,
	})
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}
	t.Cleanup(s.Close)
	ensureContractProject(t, s, ctx, "wf")

	iss := mustCreateContractIssue(t, s, ctx, "wf", "Y", 2)
	if iss.State != "backlog" {
		t.Fatalf("create default state = %q, want backlog (from config)", iss.State)
	}

	// A status that is valid under the DEFAULT config but not this custom one
	// must be rejected.
	rfr := model.IssueState("ready_for_review")
	if _, err := s.UpdateIssue(ctx, iss.ID, UpdateIssueInput{State: &rfr}); !errors.Is(err, ErrInvalidIssueState) {
		t.Fatalf("custom config should reject ready_for_review, got %v", err)
	}

	// A custom status is accepted.
	qa := model.IssueState("qa")
	if _, err := s.UpdateIssue(ctx, iss.ID, UpdateIssueInput{State: &qa}); err != nil {
		t.Fatalf("UpdateIssue to custom qa: %v", err)
	}
}

// TestReadyExcludesHoldAndBlocks proves a ready_for_* (hold) blocker neither
// surfaces in ready nor satisfies a dependent, while a terminal blocker does.
func TestReadyExcludesHoldAndBlocks(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	ensureContractProject(t, s, ctx, "wf")

	blocker := mustCreateContractIssue(t, s, ctx, "wf", "Blocker", 2)
	dependent := mustCreateContractIssue(t, s, ctx, "wf", "Dependent", 2)
	if err := s.AddDep(ctx, dependent.ID, blocker.ID); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	terminal := model.DefaultWorkflowConfig().Terminal
	active := model.DefaultWorkflowConfig().Active

	// Move the blocker into a hold state; the dependent must still be blocked.
	rfm := model.IssueState("ready_for_merge")
	if _, err := s.UpdateIssue(ctx, blocker.ID, UpdateIssueInput{State: &rfm}); err != nil {
		t.Fatalf("UpdateIssue blocker: %v", err)
	}
	ready, err := s.ReadyIssues(ctx, ListFilter{Prefix: "wf"}, terminal, active)
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	if containsIssue(ready, dependent.ID) {
		t.Error("dependent should be blocked while blocker is in a hold state")
	}
	if containsIssue(ready, blocker.ID) {
		t.Error("blocker in hold state must not be ready (not active)")
	}

	// Close the blocker (terminal); the dependent becomes ready.
	if err := s.CloseIssue(ctx, blocker.ID, "tester", ""); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	ready, err = s.ReadyIssues(ctx, ListFilter{Prefix: "wf"}, terminal, active)
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	if !containsIssue(ready, dependent.ID) {
		t.Error("dependent should be ready once blocker is terminal")
	}
}

func containsIssue(issues []Issue, id string) bool {
	for _, iss := range issues {
		if iss.ID == id {
			return true
		}
	}
	return false
}
