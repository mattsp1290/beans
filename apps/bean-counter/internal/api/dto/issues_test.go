package dto

import (
	"encoding/json"
	"testing"
	"time"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

func TestIssueFromStoreCopiesFields(t *testing.T) {
	created := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	issue := appstore.Issue{
		Issue: appstore.IssueModel{
			ID:          "bc-123",
			Identifier:  "BC-123",
			Title:       "Title",
			Description: "Description",
			Priority:    2,
			State:       "open",
			Labels:      []string{"api"},
			BlockedBy:   []string{"bc-100"},
			BranchName:  "feature/branch",
			URL:         "https://example.test/bc-123",
			Repo: &appstore.RepoTarget{
				ID:             "repo-1",
				Slug:           "bean-counter",
				RemoteURL:      "git@example.test/repo.git",
				DefaultBranch:  "main",
				RequestedRef:   "main",
				BaseRef:        "main",
				WorkBranch:     "work",
				WorktreeSubdir: "app",
				CloneStrategy:  "full",
				AuthRef:        "default",
				Metadata:       map[string]any{"team": "core"},
			},
			CreatedAt: created,
			UpdatedAt: updated,
		},
		IssueType: "feature",
	}

	got := IssueFromStore(issue)
	if got.ID != "bc-123" || got.Identifier != "BC-123" || got.Title != "Title" {
		t.Fatalf("basic fields not mapped: %+v", got)
	}
	if got.Priority != 2 || got.IssueType != "feature" || got.State != "open" {
		t.Fatalf("typed fields not mapped: %+v", got)
	}
	if got.CreatedAt != created || got.UpdatedAt != updated {
		t.Fatalf("timestamps not mapped: %+v", got)
	}
	if got.Repo == nil || got.Repo.Slug != "bean-counter" || got.Repo.Metadata["team"] != "core" {
		t.Fatalf("repo not mapped: %+v", got.Repo)
	}

	issue.Labels[0] = "mutated"
	issue.BlockedBy[0] = "mutated"
	issue.Repo.Metadata["team"] = "mutated"
	if got.Labels[0] != "api" || got.BlockedBy[0] != "bc-100" || got.Repo.Metadata["team"] != "core" {
		t.Fatalf("mapper did not copy mutable fields: %+v", got)
	}
}

func TestCreateIssueRequestToStoreInput(t *testing.T) {
	req := CreateIssueRequest{
		Title:       "New",
		Description: "Body",
		Priority:    1,
		IssueType:   "task",
		Labels:      []string{"one"},
		BranchName:  "branch",
		URL:         "https://example.test",
		Repo: &IssueRepoInput{
			RepoSlug: "repo",
			Metadata: map[string]any{"k": "v"},
		},
	}

	got := req.ToStoreInput("bc", "agent")
	if got.Prefix != "bc" || got.Actor != "agent" || got.Title != "New" || got.IssueType != "task" {
		t.Fatalf("input not mapped: %+v", got)
	}
	if got.Repo == nil || got.Repo.RepoSlug != "repo" || got.Repo.Metadata["k"] != "v" {
		t.Fatalf("repo not mapped: %+v", got.Repo)
	}

	req.Labels[0] = "mutated"
	req.Repo.Metadata["k"] = "mutated"
	if got.Labels[0] != "one" || got.Repo.Metadata["k"] != "v" {
		t.Fatalf("input mapper did not copy mutable fields: %+v", got)
	}
}

func TestUpdateIssueRequestToStoreInput(t *testing.T) {
	title := "Updated"
	state := "closed"
	labels := []string{}
	req := UpdateIssueRequest{
		Title:  &title,
		State:  &state,
		Labels: labels,
	}

	got := req.ToStoreInput()
	if got.Title == nil || *got.Title != "Updated" {
		t.Fatalf("title not mapped: %+v", got.Title)
	}
	if got.State == nil || string(*got.State) != "closed" {
		t.Fatalf("state not mapped: %+v", got.State)
	}
	if got.Labels == nil || len(got.Labels) != 0 {
		t.Fatalf("empty labels should be preserved for clearing: %+v", got.Labels)
	}
}

func TestUpdateIssueRequestLabelsJSONSemantics(t *testing.T) {
	var omitted UpdateIssueRequest
	if err := json.Unmarshal([]byte(`{}`), &omitted); err != nil {
		t.Fatalf("unmarshal omitted labels: %v", err)
	}
	if got := omitted.ToStoreInput(); got.Labels != nil {
		t.Fatalf("omitted labels = %#v, want nil", got.Labels)
	}

	var empty UpdateIssueRequest
	if err := json.Unmarshal([]byte(`{"labels":[]}`), &empty); err != nil {
		t.Fatalf("unmarshal empty labels: %v", err)
	}
	if got := empty.ToStoreInput(); got.Labels == nil || len(got.Labels) != 0 {
		t.Fatalf("empty labels = %#v, want empty non-nil slice", got.Labels)
	}

	var nullLabels UpdateIssueRequest
	if err := json.Unmarshal([]byte(`{"labels":null}`), &nullLabels); err != nil {
		t.Fatalf("unmarshal null labels: %v", err)
	}
	if got := nullLabels.ToStoreInput(); got.Labels != nil {
		t.Fatalf("null labels = %#v, want nil until validation rejects or documents null", got.Labels)
	}
}
