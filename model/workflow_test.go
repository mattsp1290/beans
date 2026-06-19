package model

import "testing"

func TestDefaultWorkflowConfigBuckets(t *testing.T) {
	w := DefaultWorkflowConfig()

	wantStatuses := []IssueState{
		"open", "in_progress", "ready_for_review", "ready_for_validation",
		"ready_for_merge", "blocked", "closed", "done",
	}
	if len(w.Statuses) != len(wantStatuses) {
		t.Fatalf("Statuses len = %d, want %d", len(w.Statuses), len(wantStatuses))
	}
	for i, s := range wantStatuses {
		if w.Statuses[i] != s {
			t.Errorf("Statuses[%d] = %q, want %q", i, w.Statuses[i], s)
		}
	}

	if w.DefaultState() != "open" {
		t.Errorf("DefaultState = %q, want open", w.DefaultState())
	}

	// The three new statuses must classify as hold: valid, not active, not terminal.
	for _, s := range []IssueState{"ready_for_review", "ready_for_validation", "ready_for_merge"} {
		if !w.IsValid(s) {
			t.Errorf("%q should be valid", s)
		}
		if w.IsActive(s) {
			t.Errorf("%q should not be active", s)
		}
		if w.IsTerminal(s) {
			t.Errorf("%q should not be terminal", s)
		}
		if !w.IsHold(s) {
			t.Errorf("%q should be hold", s)
		}
	}

	if !w.IsActive("open") || w.IsHold("open") {
		t.Errorf("open should be active, not hold")
	}
	for _, s := range []IssueState{"closed", "done"} {
		if !w.IsTerminal(s) || w.IsHold(s) {
			t.Errorf("%q should be terminal, not hold", s)
		}
	}
}

func TestWorkflowConfigUnknownStatusNotHold(t *testing.T) {
	w := DefaultWorkflowConfig()
	// Read-tolerance: an unknown status is invalid and therefore NOT hold; callers
	// classify it conservatively (excluded from dispatch, never terminal).
	if w.IsValid("mystery") {
		t.Fatal("unknown status should be invalid")
	}
	if w.IsActive("mystery") || w.IsTerminal("mystery") || w.IsHold("mystery") {
		t.Error("unknown status must not classify as active/terminal/hold")
	}
}

func TestWorkflowConfigDefaultStateFallback(t *testing.T) {
	var zero WorkflowConfig // defensive zero value
	if zero.DefaultState() != "open" {
		t.Errorf("zero-value DefaultState = %q, want open", zero.DefaultState())
	}
}

func TestWorkflowConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WorkflowConfig
		wantErr bool
	}{
		{
			name: "default is valid",
			cfg:  DefaultWorkflowConfig(),
		},
		{
			name:    "empty statuses",
			cfg:     WorkflowConfig{Default: "open"},
			wantErr: true,
		},
		{
			name:    "default not in statuses",
			cfg:     WorkflowConfig{Statuses: []IssueState{"open"}, Default: "closed"},
			wantErr: true,
		},
		{
			name:    "active not in statuses",
			cfg:     WorkflowConfig{Statuses: []IssueState{"open"}, Default: "open", Active: []IssueState{"weird"}},
			wantErr: true,
		},
		{
			name:    "terminal not in statuses",
			cfg:     WorkflowConfig{Statuses: []IssueState{"open"}, Default: "open", Terminal: []IssueState{"weird"}},
			wantErr: true,
		},
		{
			name: "active and terminal overlap",
			cfg: WorkflowConfig{
				Statuses: []IssueState{"open", "closed"},
				Default:  "open",
				Active:   []IssueState{"open", "closed"},
				Terminal: []IssueState{"closed"},
			},
			wantErr: true,
		},
		{
			name:    "duplicate status",
			cfg:     WorkflowConfig{Statuses: []IssueState{"open", "open"}, Default: "open"},
			wantErr: true,
		},
		{
			name: "transition target not in statuses",
			cfg: WorkflowConfig{
				Statuses:    []IssueState{"open", "closed"},
				Default:     "open",
				Transitions: map[IssueState][]IssueState{"open": {"ghost"}},
			},
			wantErr: true,
		},
		{
			name: "valid custom config",
			cfg: WorkflowConfig{
				Statuses: []IssueState{"open", "qa", "closed"},
				Default:  "open",
				Active:   []IssueState{"open"},
				Terminal: []IssueState{"closed"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
