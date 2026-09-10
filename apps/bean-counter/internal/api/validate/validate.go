package validate

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	beansrepo "github.com/mattsp1290/beans/repo"

	"github.com/mattsp1290/bean-counter/internal/api/dto"
	"github.com/mattsp1290/bean-counter/internal/server"
)

const (
	MaxIDLength          = 200
	MaxTitleLength       = 300
	MaxDescriptionLength = 20000
	MaxLabelCount        = 100
	MaxLabelLength       = 100
	MaxBranchNameLength  = 255
	MaxURLLength         = 2048
	MaxReasonLength      = 1000
	MaxRepoSlugLength    = 200
	MaxRepoRefLength     = 255
	MaxWorktreeSubdir    = 255
)

var allowedIssueTypes = map[string]struct{}{
	"bug":     {},
	"feature": {},
	"task":    {},
	"epic":    {},
	"chore":   {},
}

var allowedStates = map[string]struct{}{
	"open":        {},
	"in_progress": {},
	"blocked":     {},
	"closed":      {},
	"done":        {},
}

func IssueID(id string) error {
	var fields fieldErrors
	fields.requireTrimmed("id", id, "is required")
	fields.maxLength("id", id, MaxIDLength)
	return fields.err()
}

func ProjectIssueID(prefix, id string) error {
	return ProjectIssueIDField("id", prefix, id)
}

func ProjectIssueIDField(field, prefix, id string) error {
	var fields fieldErrors
	fields.id(field, id)
	prefix = strings.TrimSpace(prefix)
	id = strings.TrimSpace(id)
	if prefix != "" && id != "" && !strings.HasPrefix(id, prefix+"-") {
		fields.add(field, "must use configured project prefix")
	}
	return fields.err()
}

func IssueState(field, state string) error {
	var fields fieldErrors
	fields.state(field, state)
	return fields.err()
}

func CreateIssueBody(body []byte, req dto.CreateIssueRequest) error {
	var fields fieldErrors
	if !jsonObjectHas(body, "priority") {
		fields.add("priority", "is required")
	}
	fields.append(CreateIssue(req))
	return fields.err()
}

func CreateIssue(req dto.CreateIssueRequest) error {
	var fields fieldErrors
	fields.requireTrimmed("title", req.Title, "is required")
	fields.maxLength("title", req.Title, MaxTitleLength)
	fields.maxLength("description", req.Description, MaxDescriptionLength)
	fields.priority("priority", req.Priority)
	fields.issueType("issue_type", req.IssueType)
	fields.labels("labels", req.Labels)
	fields.maxLength("branch_name", req.BranchName, MaxBranchNameLength)
	fields.httpURL("url", req.URL)
	fields.repo("repo", req.Repo)
	return fields.err()
}

func UpdateIssueBody(body []byte, req dto.UpdateIssueRequest) error {
	var fields fieldErrors
	if jsonObjectHasNull(body, "labels") {
		fields.add("labels", "cannot be null")
	}
	fields.append(UpdateIssue(req))
	return fields.err()
}

func UpdateIssue(req dto.UpdateIssueRequest) error {
	var fields fieldErrors
	hasField := false
	if req.Title != nil {
		hasField = true
		fields.requireTrimmed("title", *req.Title, "cannot be blank when provided")
		fields.maxLength("title", *req.Title, MaxTitleLength)
	}
	if req.Description != nil {
		hasField = true
		fields.maxLength("description", *req.Description, MaxDescriptionLength)
	}
	if req.Priority != nil {
		hasField = true
		fields.priority("priority", *req.Priority)
	}
	if req.State != nil {
		hasField = true
		fields.state("state", *req.State)
	}
	if req.Labels != nil {
		hasField = true
		fields.labels("labels", req.Labels)
	}
	if req.BranchName != nil {
		hasField = true
		fields.maxLength("branch_name", *req.BranchName, MaxBranchNameLength)
	}
	if req.URL != nil {
		hasField = true
		fields.httpURL("url", *req.URL)
	}
	if req.Repo != nil {
		hasField = true
		fields.repo("repo", req.Repo)
	}
	if !hasField {
		fields.add("", "at least one field is required")
	}
	return fields.err()
}

func CloseIssue(req dto.CloseIssueRequest) error {
	var fields fieldErrors
	fields.maxLength("reason", req.Reason, MaxReasonLength)
	return fields.err()
}

func AddDependency(issueID string, req dto.AddDependencyRequest) error {
	var fields fieldErrors
	fields.id("id", issueID)
	fields.id("blocked_by_id", req.BlockedByID)
	if strings.TrimSpace(issueID) != "" &&
		strings.TrimSpace(req.BlockedByID) != "" &&
		strings.TrimSpace(issueID) == strings.TrimSpace(req.BlockedByID) {
		fields.add("blocked_by_id", "must be different from id")
	}
	return fields.err()
}

func RemoveDependency(issueID, blockedByID string) error {
	var fields fieldErrors
	fields.id("id", issueID)
	fields.id("blocked_by_id", blockedByID)
	if strings.TrimSpace(issueID) != "" &&
		strings.TrimSpace(blockedByID) != "" &&
		strings.TrimSpace(issueID) == strings.TrimSpace(blockedByID) {
		fields.add("blocked_by_id", "must be different from id")
	}
	return fields.err()
}

type fieldErrors []server.FieldError

func (f *fieldErrors) add(field, message string) {
	*f = append(*f, server.FieldError{Field: field, Message: message})
}

func (f *fieldErrors) append(err error) {
	if err == nil {
		return
	}
	var validation server.ValidationError
	if !errors.As(err, &validation) {
		f.add("", err.Error())
		return
	}
	*f = append(*f, validation.Fields...)
}

func (f *fieldErrors) err() error {
	if len(*f) == 0 {
		return nil
	}
	return server.ValidationError{
		Message: "invalid request",
		Fields:  []server.FieldError(*f),
	}
}

func (f *fieldErrors) id(field, value string) {
	f.requireTrimmed(field, value, "is required")
	f.maxLength(field, value, MaxIDLength)
}

func (f *fieldErrors) requireTrimmed(field, value, message string) {
	if strings.TrimSpace(value) == "" {
		f.add(field, message)
	}
}

func (f *fieldErrors) maxLength(field, value string, max int) {
	if len(value) > max {
		f.add(field, "must be at most "+itoa(max)+" characters")
	}
}

func (f *fieldErrors) priority(field string, value int) {
	if value < 0 || value > 4 {
		f.add(field, "must be between 0 and 4")
	}
}

func (f *fieldErrors) issueType(field, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		f.add(field, "is required")
		return
	}
	if _, ok := allowedIssueTypes[value]; !ok {
		f.add(field, "must be one of bug, feature, task, epic, chore")
	}
}

func (f *fieldErrors) state(field, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		f.add(field, "cannot be blank when provided")
		return
	}
	if _, ok := allowedStates[value]; !ok {
		f.add(field, "must be one of open, in_progress, blocked, closed, done")
	}
}

func (f *fieldErrors) labels(field string, labels []string) {
	if len(labels) > MaxLabelCount {
		f.add(field, "must contain at most "+itoa(MaxLabelCount)+" labels")
	}
	for i, label := range labels {
		itemField := field + "[" + itoa(i) + "]"
		f.requireTrimmed(itemField, label, "cannot be blank")
		f.maxLength(itemField, label, MaxLabelLength)
	}
}

func (f *fieldErrors) httpURL(field, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	f.maxLength(field, value, MaxURLLength)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		f.add(field, "must be an absolute http or https URL")
	}
}

func (f *fieldErrors) repo(prefix string, repo *dto.IssueRepoInput) {
	if repo == nil {
		return
	}
	f.requireTrimmed(prefix+".repo_slug", repo.RepoSlug, "is required")
	f.maxLength(prefix+".repo_slug", repo.RepoSlug, MaxRepoSlugLength)
	f.repoRef(prefix+".requested_ref", repo.RequestedRef)
	f.repoRef(prefix+".base_ref", repo.BaseRef)
	f.repoRef(prefix+".work_branch", repo.WorkBranch)
	f.worktreeSubdir(prefix+".worktree_subdir", repo.WorktreeSubdir)
}

func (f *fieldErrors) repoRef(field, value string) {
	f.maxLength(field, value, MaxRepoRefLength)
	if err := beansrepo.ValidateDefaultBranch(value); err != nil {
		f.add(field, "must be a valid git ref name")
	}
}

func (f *fieldErrors) worktreeSubdir(field, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	f.maxLength(field, value, MaxWorktreeSubdir)
	if err := beansrepo.ValidateWorktreeSubdir(value); err != nil {
		f.add(field, "must be a relative path without parent traversal")
	}
}

func jsonObjectHas(body []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

func jsonObjectHasNull(body []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	value, ok := raw[key]
	return ok && string(value) == "null"
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
