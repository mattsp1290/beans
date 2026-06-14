package validate

import (
	"errors"
	"strings"
	"testing"

	"github.com/mattsp1290/bean-counter/internal/api/dto"
	"github.com/mattsp1290/bean-counter/internal/server"
)

func TestCreateIssueAcceptsValidRequest(t *testing.T) {
	err := CreateIssue(dto.CreateIssueRequest{
		Title:     "Create UI",
		Priority:  2,
		IssueType: "feature",
		Labels:    []string{"ui"},
		URL:       "https://example.test/issues/1",
		Repo: &dto.IssueRepoInput{
			RepoSlug:       "bean-counter",
			RequestedRef:   "main",
			BaseRef:        "main",
			WorkBranch:     "feature/create-ui",
			WorktreeSubdir: "frontend",
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue error = %v", err)
	}
}

func TestCreateIssueReturnsFieldErrors(t *testing.T) {
	err := CreateIssue(dto.CreateIssueRequest{
		Title:     " ",
		Priority:  5,
		IssueType: "story",
		Labels:    []string{""},
		URL:       "ftp://example.test/issue",
		Repo: &dto.IssueRepoInput{
			WorktreeSubdir: "../outside",
		},
	})

	fields := validationFields(t, err)
	wantFields(t, fields,
		"title",
		"priority",
		"issue_type",
		"labels[0]",
		"url",
		"repo.repo_slug",
		"repo.worktree_subdir",
	)
}

func TestCreateIssueBodyRequiresPriorityPresence(t *testing.T) {
	req := dto.CreateIssueRequest{
		Title:     "Create UI",
		IssueType: "feature",
	}
	fields := validationFields(t, CreateIssueBody([]byte(`{"title":"Create UI","issue_type":"feature"}`), req))
	wantFields(t, fields, "priority")
}

func TestUpdateIssueRequiresAtLeastOneField(t *testing.T) {
	fields := validationFields(t, UpdateIssue(dto.UpdateIssueRequest{}))
	wantFields(t, fields, "")
}

func TestUpdateIssueBodyRejectsNullLabels(t *testing.T) {
	title := "Update"
	req := dto.UpdateIssueRequest{Title: &title}
	fields := validationFields(t, UpdateIssueBody([]byte(`{"title":"Update","labels":null}`), req))
	wantFields(t, fields, "labels")
}

func TestUpdateIssueValidatesProvidedFields(t *testing.T) {
	title := ""
	priority := -1
	state := "archived"
	url := "notaurl"
	branch := strings.Repeat("b", MaxBranchNameLength+1)
	err := UpdateIssue(dto.UpdateIssueRequest{
		Title:      &title,
		Priority:   &priority,
		State:      &state,
		URL:        &url,
		BranchName: &branch,
		Labels:     []string{strings.Repeat("l", MaxLabelLength+1)},
		Repo:       &dto.IssueRepoInput{RepoSlug: "repo"},
	})

	fields := validationFields(t, err)
	wantFields(t, fields, "title", "priority", "state", "labels[0]", "branch_name", "url")
}

func TestRepoValidationMirrorsBeansRefAndSubdirRules(t *testing.T) {
	err := CreateIssue(dto.CreateIssueRequest{
		Title:     "Create UI",
		Priority:  2,
		IssueType: "feature",
		Repo: &dto.IssueRepoInput{
			RepoSlug:       "repo",
			RequestedRef:   "-bad",
			BaseRef:        "bad\nbranch",
			WorkBranch:     "bad\rbranch",
			WorktreeSubdir: "bad\x00dir",
		},
	})
	fields := validationFields(t, err)
	wantFields(t, fields, "repo.requested_ref", "repo.base_ref", "repo.work_branch", "repo.worktree_subdir")
}

func TestCloseIssueValidatesReasonLength(t *testing.T) {
	fields := validationFields(t, CloseIssue(dto.CloseIssueRequest{
		Reason: strings.Repeat("r", MaxReasonLength+1),
	}))
	wantFields(t, fields, "reason")
}

func TestDependencyValidation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "add missing blocked by",
			err:  AddDependency("bc-1", dto.AddDependencyRequest{}),
			want: []string{"blocked_by_id"},
		},
		{
			name: "add self dependency",
			err:  AddDependency("bc-1", dto.AddDependencyRequest{BlockedByID: "bc-1"}),
			want: []string{"blocked_by_id"},
		},
		{
			name: "remove missing id",
			err:  RemoveDependency("", "bc-1"),
			want: []string{"id"},
		},
		{
			name: "remove valid",
			err:  RemoveDependency("bc-2", "bc-1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.want) == 0 {
				if tt.err != nil {
					t.Fatalf("error = %v, want nil", tt.err)
				}
				return
			}
			wantFields(t, validationFields(t, tt.err), tt.want...)
		})
	}
}

func TestIssueID(t *testing.T) {
	if err := IssueID("bc-1"); err != nil {
		t.Fatalf("IssueID valid error = %v", err)
	}
	wantFields(t, validationFields(t, IssueID(" ")), "id")
}

func validationFields(t *testing.T, err error) []server.FieldError {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want validation error")
	}
	if !errors.Is(err, server.ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	var validation server.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T, want ValidationError", err)
	}
	return validation.Fields
}

func wantFields(t *testing.T, fields []server.FieldError, want ...string) {
	t.Helper()

	got := map[string]bool{}
	for _, field := range fields {
		got[field.Field] = true
	}
	for _, field := range want {
		if !got[field] {
			t.Fatalf("fields = %+v, missing %q", fields, field)
		}
	}
}
