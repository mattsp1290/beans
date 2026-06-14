package dto

import (
	"time"

	beansmodel "github.com/mattsp1290/beans/model"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type Issue struct {
	ID          string      `json:"id"`
	Identifier  string      `json:"identifier"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Priority    int         `json:"priority"`
	IssueType   string      `json:"issue_type"`
	State       string      `json:"state"`
	Labels      []string    `json:"labels,omitempty"`
	BlockedBy   []string    `json:"blocked_by,omitempty"`
	BranchName  string      `json:"branch_name,omitempty"`
	URL         string      `json:"url,omitempty"`
	Repo        *RepoTarget `json:"repo,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type RepoTarget struct {
	ID             string         `json:"id,omitempty"`
	Slug           string         `json:"slug"`
	RemoteURL      string         `json:"remote_url,omitempty"`
	DefaultBranch  string         `json:"default_branch,omitempty"`
	RequestedRef   string         `json:"requested_ref,omitempty"`
	BaseRef        string         `json:"base_ref,omitempty"`
	WorkBranch     string         `json:"work_branch,omitempty"`
	WorktreeSubdir string         `json:"worktree_subdir,omitempty"`
	CloneStrategy  string         `json:"clone_strategy,omitempty"`
	AuthRef        string         `json:"auth_ref,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type IssueListResponse struct {
	Issues []Issue `json:"issues"`
}

type CreateIssueRequest struct {
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Priority    int             `json:"priority"`
	IssueType   string          `json:"issue_type"`
	Labels      []string        `json:"labels,omitempty"`
	BranchName  string          `json:"branch_name,omitempty"`
	URL         string          `json:"url,omitempty"`
	Repo        *IssueRepoInput `json:"repo,omitempty"`
}

type IssueRepoInput struct {
	RepoSlug       string         `json:"repo_slug"`
	RequestedRef   string         `json:"requested_ref,omitempty"`
	BaseRef        string         `json:"base_ref,omitempty"`
	WorkBranch     string         `json:"work_branch,omitempty"`
	WorktreeSubdir string         `json:"worktree_subdir,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type UpdateIssueRequest struct {
	Title       *string         `json:"title,omitempty"`
	Description *string         `json:"description,omitempty"`
	Priority    *int            `json:"priority,omitempty"`
	State       *string         `json:"state,omitempty"`
	Labels      []string        `json:"labels,omitempty"`
	BranchName  *string         `json:"branch_name,omitempty"`
	URL         *string         `json:"url,omitempty"`
	Repo        *IssueRepoInput `json:"repo,omitempty"`
}

type CloseIssueRequest struct {
	Reason string `json:"reason,omitempty"`
}

func IssueFromStore(issue appstore.Issue) Issue {
	return Issue{
		ID:          issue.ID,
		Identifier:  issue.Identifier,
		Title:       issue.Title,
		Description: issue.Description,
		Priority:    int(issue.Priority),
		IssueType:   issue.IssueType,
		State:       string(issue.State),
		Labels:      append([]string(nil), issue.Labels...),
		BlockedBy:   append([]string(nil), issue.BlockedBy...),
		BranchName:  issue.BranchName,
		URL:         issue.URL,
		Repo:        RepoTargetFromStore(issue.Repo),
		CreatedAt:   issue.CreatedAt,
		UpdatedAt:   issue.UpdatedAt,
	}
}

func IssuesFromStore(issues []appstore.Issue) []Issue {
	result := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, IssueFromStore(issue))
	}
	return result
}

func RepoTargetFromStore(repo *beansmodel.RepoTarget) *RepoTarget {
	if repo == nil {
		return nil
	}
	return &RepoTarget{
		ID:             repo.ID,
		Slug:           repo.Slug,
		RemoteURL:      repo.RemoteURL,
		DefaultBranch:  repo.DefaultBranch,
		RequestedRef:   repo.RequestedRef,
		BaseRef:        repo.BaseRef,
		WorkBranch:     repo.WorkBranch,
		WorktreeSubdir: repo.WorktreeSubdir,
		CloneStrategy:  repo.CloneStrategy,
		AuthRef:        repo.AuthRef,
		Metadata:       copyMap(repo.Metadata),
	}
}

func (r CreateIssueRequest) ToStoreInput(prefix, actor string) appstore.CreateIssueInput {
	return appstore.CreateIssueInput{
		Prefix:      prefix,
		Title:       r.Title,
		Description: r.Description,
		Priority:    r.Priority,
		IssueType:   r.IssueType,
		Labels:      append([]string(nil), r.Labels...),
		BranchName:  r.BranchName,
		URL:         r.URL,
		Actor:       actor,
		Repo:        r.Repo.toStoreInput(),
	}
}

func (r UpdateIssueRequest) ToStoreInput() appstore.UpdateIssueInput {
	var state *beansmodel.IssueState
	if r.State != nil {
		converted := beansmodel.IssueState(*r.State)
		state = &converted
	}
	return appstore.UpdateIssueInput{
		Title:       r.Title,
		Description: r.Description,
		Priority:    r.Priority,
		State:       state,
		Labels:      copyOptionalSlice(r.Labels),
		BranchName:  r.BranchName,
		URL:         r.URL,
		Repo:        r.Repo.toStoreInput(),
	}
}

func (r *IssueRepoInput) toStoreInput() *appstore.IssueRepoInput {
	if r == nil {
		return nil
	}
	return &appstore.IssueRepoInput{
		RepoSlug:       r.RepoSlug,
		RequestedRef:   r.RequestedRef,
		BaseRef:        r.BaseRef,
		WorkBranch:     r.WorkBranch,
		WorktreeSubdir: r.WorktreeSubdir,
		Metadata:       copyMap(r.Metadata),
	}
}

func copyOptionalSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func copyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
