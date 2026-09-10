package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPriorityValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		p    Priority
		want bool
	}{
		{"unset", PriorityUnset, true},
		{"critical", PriorityCritical, true},
		{"high", PriorityHigh, true},
		{"medium", PriorityMedium, true},
		{"low", PriorityLow, true},
		{"backlog", PriorityBacklog, true},
		{"negative", -1, false},
		{"way-low", -100, false},
		{"too-high", 6, false},
		{"way-out", 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.want {
				t.Errorf("Priority(%d).Valid() = %v, want %v", tc.p, got, tc.want)
			}
		})
	}
}

// TestPriorityZeroValueIsUnset locks in the zero-value contract: a
// default-constructed Issue must NOT silently become PriorityCritical. This is
// the textbook anti-pattern from go-domain-types-and-enums.md and the lock-in
// matters for downstream tracker adapters that may forget to set the field.
func TestPriorityZeroValueIsUnset(t *testing.T) {
	t.Parallel()
	var p Priority
	if p != PriorityUnset {
		t.Fatalf("zero-value Priority = %d, want PriorityUnset (%d)", p, PriorityUnset)
	}
	if PriorityCritical == 0 {
		t.Fatal("PriorityCritical must NOT be the zero value; refactor would silently elevate every default-constructed Issue")
	}
	// Sanity-check the documented sort contract: PriorityUnset is numerically
	// the smallest, but the orchestrator sort treats it as "null last." The
	// orchestrator owns that sort; the type itself just defines the values.
	if PriorityUnset >= PriorityCritical {
		t.Fatal("PriorityUnset must be numerically less than PriorityCritical; orchestrator inverts at sort time")
	}
}

func TestIssueJSONRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	in := Issue{
		ID:          "beans-0cn",
		Identifier:  "beans-0cn",
		Title:       "Initialize Go module",
		Description: "...",
		Priority:    PriorityCritical,
		State:       IssueState("open"),
		BranchName:  "feature/init",
		URL:         "https://example.invalid/issue/0cn",
		Labels:      []string{"setup", "core"},
		BlockedBy:   nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out Issue
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Title != in.Title || out.Priority != in.Priority || out.State != in.State {
		t.Fatalf("round trip mismatch: in=%+v out=%+v", in, out)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) {
		t.Fatalf("CreatedAt mismatch: in=%v out=%v", in.CreatedAt, out.CreatedAt)
	}
}

func TestIssueOmitemptyKeepsZeroFieldsOut(t *testing.T) {
	t.Parallel()
	in := Issue{
		ID:    "x",
		State: IssueState("open"),
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(raw)
	for _, banned := range []string{`"branch_name"`, `"url"`, `"labels"`, `"blocked_by"`} {
		if strings.Contains(got, banned) {
			t.Errorf("expected %s to be omitted, got %s", banned, got)
		}
	}
}
