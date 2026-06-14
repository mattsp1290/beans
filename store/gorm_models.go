package store

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/datatypes"

	"github.com/mattsp1290/beans/model"
)

// GORM table models mirror the migration-owned bn_* schema. They deliberately
// avoid dialect-specific column type tags; migrations remain the source of
// constraints, indexes, and generated columns.

type gormProject struct {
	Prefix    string   `gorm:"column:prefix;primaryKey"`
	CreatedAt gormTime `gorm:"column:created_at;not null"`
}

func (gormProject) TableName() string { return "bn_projects" }

type gormIssue struct {
	ID          string         `gorm:"column:id;primaryKey"`
	Prefix      string         `gorm:"column:prefix;not null"`
	Identifier  *string        `gorm:"column:identifier"`
	Title       string         `gorm:"column:title;not null"`
	Description string         `gorm:"column:description;not null"`
	Priority    int            `gorm:"column:priority;not null"`
	IssueType   string         `gorm:"column:issue_type;not null"`
	State       string         `gorm:"column:state;not null"`
	Labels      datatypes.JSON `gorm:"column:labels;not null"`
	BranchName  *string        `gorm:"column:branch_name"`
	URL         *string        `gorm:"column:url"`
	CreatedAt   gormTime       `gorm:"column:created_at;not null"`
	UpdatedAt   gormTime       `gorm:"column:updated_at;not null"`
}

func (gormIssue) TableName() string { return "bn_issues" }

type gormIssueDep struct {
	IssueID     string `gorm:"column:issue_id;primaryKey"`
	BlockedByID string `gorm:"column:blocked_by_id;primaryKey"`
}

func (gormIssueDep) TableName() string { return "bn_issue_deps" }

type gormDepGraphGuard struct {
	ID        int16    `gorm:"column:id;primaryKey"`
	UpdatedAt gormTime `gorm:"column:updated_at;not null"`
}

func (gormDepGraphGuard) TableName() string { return "bn_dep_graph_guard" }

type gormIssueNote struct {
	ID        int64    `gorm:"column:id;primaryKey;autoIncrement"`
	IssueID   string   `gorm:"column:issue_id;not null"`
	Actor     *string  `gorm:"column:actor"`
	Body      string   `gorm:"column:body;not null"`
	CreatedAt gormTime `gorm:"column:created_at;not null"`
}

func (gormIssueNote) TableName() string { return "bn_issue_notes" }

type gormMemory struct {
	ID        int64          `gorm:"column:id;primaryKey;autoIncrement"`
	Prefix    *string        `gorm:"column:prefix"`
	Body      string         `gorm:"column:body;not null"`
	MType     *string        `gorm:"column:mtype"`
	Tags      datatypes.JSON `gorm:"column:tags;not null"`
	CreatedAt gormTime       `gorm:"column:created_at;not null"`
}

func (gormMemory) TableName() string { return "bn_memories" }

type gormMemoryTag struct {
	MemoryID int64  `gorm:"column:memory_id;primaryKey"`
	Tag      string `gorm:"column:tag;primaryKey;not null"`
}

func (gormMemoryTag) TableName() string { return "bn_memory_tags" }

type gormRepo struct {
	ID             string         `gorm:"column:id;primaryKey"`
	Prefix         string         `gorm:"column:prefix;not null"`
	Slug           string         `gorm:"column:slug;not null"`
	DisplayName    string         `gorm:"column:display_name;not null"`
	RemoteURL      string         `gorm:"column:remote_url;not null"`
	DefaultBranch  string         `gorm:"column:default_branch;not null"`
	WorktreeSubdir string         `gorm:"column:worktree_subdir;not null"`
	CloneStrategy  string         `gorm:"column:clone_strategy;not null"`
	AuthRef        string         `gorm:"column:auth_ref;not null"`
	Enabled        bool           `gorm:"column:enabled;not null"`
	Metadata       datatypes.JSON `gorm:"column:metadata;not null"`
	CreatedAt      gormTime       `gorm:"column:created_at;not null"`
	UpdatedAt      gormTime       `gorm:"column:updated_at;not null"`
	CreatedBy      string         `gorm:"column:created_by;not null"`
	UpdatedBy      string         `gorm:"column:updated_by;not null"`
}

func (gormRepo) TableName() string { return "bn_repos" }

type gormRepoAlias struct {
	Prefix    string   `gorm:"column:prefix;primaryKey"`
	Alias     string   `gorm:"column:alias;primaryKey"`
	RepoID    string   `gorm:"column:repo_id;not null"`
	CreatedAt gormTime `gorm:"column:created_at;not null"`
}

func (gormRepoAlias) TableName() string { return "bn_repo_aliases" }

type gormProjectAdmin struct {
	Prefix    string   `gorm:"column:prefix;primaryKey"`
	Actor     string   `gorm:"column:actor;primaryKey"`
	CreatedAt gormTime `gorm:"column:created_at;not null"`
}

func (gormProjectAdmin) TableName() string { return "bn_project_admins" }

type gormProjectAdminBootstrap struct {
	Prefix    string   `gorm:"column:prefix;primaryKey"`
	Actor     string   `gorm:"column:actor;not null"`
	CreatedAt gormTime `gorm:"column:created_at;not null"`
}

func (gormProjectAdminBootstrap) TableName() string { return "bn_project_admin_bootstraps" }

type gormRepoAudit struct {
	ID        int64          `gorm:"column:id;primaryKey;autoIncrement"`
	Prefix    string         `gorm:"column:prefix;not null"`
	RepoID    *string        `gorm:"column:repo_id"`
	Action    string         `gorm:"column:action;not null"`
	Actor     string         `gorm:"column:actor;not null"`
	OldValues datatypes.JSON `gorm:"column:old_values;not null"`
	NewValues datatypes.JSON `gorm:"column:new_values;not null"`
	Command   string         `gorm:"column:command;not null"`
	CreatedAt gormTime       `gorm:"column:created_at;not null"`
}

func (gormRepoAudit) TableName() string { return "bn_repo_audit" }

type gormIssueRepo struct {
	IssueID        string         `gorm:"column:issue_id;primaryKey"`
	RepoID         string         `gorm:"column:repo_id;not null"`
	RequestedRef   string         `gorm:"column:requested_ref;not null"`
	BaseRef        string         `gorm:"column:base_ref;not null"`
	WorkBranch     string         `gorm:"column:work_branch;not null"`
	WorktreeSubdir string         `gorm:"column:worktree_subdir;not null"`
	Metadata       datatypes.JSON `gorm:"column:metadata;not null"`
	CreatedAt      gormTime       `gorm:"column:created_at;not null"`
	UpdatedAt      gormTime       `gorm:"column:updated_at;not null"`
}

func (gormIssueRepo) TableName() string { return "bn_issue_repos" }

type gormTableModel interface {
	TableName() string
}

var allGORMModels = []gormTableModel{
	gormProject{},
	gormIssue{},
	gormIssueDep{},
	gormDepGraphGuard{},
	gormIssueNote{},
	gormMemory{},
	gormMemoryTag{},
	gormRepo{},
	gormRepoAlias{},
	gormProjectAdmin{},
	gormProjectAdminBootstrap{},
	gormRepoAudit{},
	gormIssueRepo{},
}

type gormTime struct {
	time.Time
}

func newGORMTime(t time.Time) gormTime {
	return gormTime{Time: t.UTC()}
}

func (t *gormTime) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		t.Time = time.Time{}
		return nil
	case time.Time:
		t.Time = v.UTC()
		return nil
	case string:
		return t.scanString(v)
	case []byte:
		return t.scanString(string(v))
	default:
		return fmt.Errorf("unsupported timestamp type %T", value)
	}
}

func (t gormTime) Value() (driver.Value, error) {
	return t.UTC(), nil
}

func (t *gormTime) scanString(value string) error {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	return fmt.Errorf("parse timestamp %q", value)
}

func jsonStringArray(values []string) datatypes.JSON {
	if len(values) == 0 {
		return datatypes.JSON([]byte(`[]`))
	}
	b, err := json.Marshal(values)
	if err != nil {
		return datatypes.JSON([]byte(`[]`))
	}
	return datatypes.JSON(b)
}

func stringArrayFromJSON(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func jsonObject(values map[string]any) (datatypes.JSON, error) {
	if values == nil {
		values = map[string]any{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, fmt.Errorf("invalid json object")
	}
	return datatypes.JSON(b), nil
}

func objectFromJSON(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]any{}
	}
	if values == nil {
		return map[string]any{}
	}
	return values
}

func corePriorityToStore(p model.Priority) (int, bool) {
	if !p.Valid() || p == model.PriorityUnset {
		return 0, false
	}
	return int(p - 1), true
}
