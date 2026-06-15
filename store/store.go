package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mattsp1290/beans/model"
	repovalidation "github.com/mattsp1290/beans/repo"
	"github.com/mattsp1290/beans/schema"
)

// Issue is the richer store-layer representation. It embeds model.Issue (the
// shared issue-domain shape used by callers) and adds IssueType, which the bn
// CLI needs for --json output.
type Issue struct {
	model.Issue
	IssueType string
}

// Store is the data access layer for the bn tracker. It owns the database pool
// and all queries. The bn CLI (cmd/bn) calls it directly.
//
// Store is safe for concurrent use; the underlying database pool handles
// concurrency.
type Store struct {
	p *pool
}

// New dials the configured database, runs migrations, and returns a ready Store.
// The caller must call Close when done to release pool connections.
func New(ctx context.Context, cfg Config) (*Store, error) {
	p, err := newPool(ctx, cfg)
	if err != nil {
		return nil, err
	}

	migrationDB, err := p.sql()
	if err != nil {
		p.close()
		return nil, err
	}
	if err := schema.Migrate(ctx, migrationDB, cfg.schemaDriver()); err != nil {
		p.close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return &Store{p: p}, nil
}

// Close releases all pool connections. Idempotent.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.p.close()
}

// ---------------------------------------------------------------------------
// Project management
// ---------------------------------------------------------------------------

// EnsureProject creates the project prefix if it does not exist.
// Idempotent: calling EnsureProject on an already-registered prefix is a no-op.
func (s *Store) EnsureProject(ctx context.Context, prefix string) error {
	db, err := s.p.gorm()
	if err != nil {
		return err
	}
	err = db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&gormProject{Prefix: prefix, CreatedAt: newGORMTime(clockNowUTC())}).
		Error
	return wrapExecErr(err, "EnsureProject")
}

// ProjectExists reports whether a project prefix is registered.
func (s *Store) ProjectExists(ctx context.Context, prefix string) (bool, error) {
	db, err := s.p.gorm()
	if err != nil {
		return false, err
	}
	var count int64
	err = db.WithContext(ctx).Model(&gormProject{}).Where("prefix = ?", prefix).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("store: ProjectExists: %w", err)
	}
	return count > 0, nil
}

// ---------------------------------------------------------------------------
// Issue CRUD
// ---------------------------------------------------------------------------

// CreateIssueInput carries the fields set at creation time.
type CreateIssueInput struct {
	Prefix      string
	Title       string
	Description string
	Priority    int    // 0–4; 2 is medium
	IssueType   string // bug|feature|task|epic|chore
	Labels      []string
	BranchName  string
	URL         string
	Actor       string
	Repo        *IssueRepoInput
}

// IssueRepoInput attaches repo routing metadata to an issue.
//
// Topology-a resolution in CreateIssue:
//  - RemoteURL set: AutoRegisterRepo runs before the issue transaction and the
//    issue prefix is derived from the registered repo's Prefix field.
//  - RemoteURL empty, RepoSlug set: legacy path — CallerPrefix must be supplied
//    via CreateIssueInput.Prefix, and the repo is looked up by (prefix, slug).
//  - Both empty: no repo link is created; CreateIssueInput.Prefix is used.
//
// RemoteURL is honored only by CreateIssue.  UpdateIssue ignores it — pass
// RepoSlug to re-link an issue to a repo after creation.
type IssueRepoInput struct {
	// RemoteURL is an optional remote URL (any transport form; Create-only).
	// When set, AutoRegisterRepo is called before the issue transaction so the
	// bn_projects FK is satisfied and the issue prefix is derived from the repo.
	// Ignored by UpdateIssue.
	RemoteURL      string
	RepoSlug       string
	RequestedRef   string
	BaseRef        string
	WorkBranch     string
	WorktreeSubdir string
	Metadata       map[string]any
}

// CreateIssue inserts a new issue and returns it with a generated ID.
//
// When in.Repo is provided with a RemoteURL or RepoSlug, the issue prefix is
// derived from the resolved repo (topology-a: prefix == slug).  The caller
// must either set in.Prefix explicitly (for prefix-only creation with no repo)
// or supply repo info from which the prefix is derived.
//
// If in.Repo.RemoteURL is set, AutoRegisterRepo runs BEFORE the issue
// transaction to ensure the bn_projects row exists (required by the FK on
// bn_issues.prefix).
//
// Retries on the rare hash collision (PK violation).
func (s *Store) CreateIssue(ctx context.Context, in CreateIssueInput) (Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return Issue{}, err
	}

	// Resolve the repo before the ID generation loop so the derived prefix is
	// stable across retries.  AutoRegisterRepo commits its own transaction and
	// ensures the bn_projects row exists before we open the issue transaction.
	//
	// When RemoteURL is provided (topology-a), the issue prefix is derived from
	// the registered repo (prefix == slug).  When only RepoSlug is provided
	// (legacy path), in.Prefix is used verbatim — insertIssueRepoGORM will look
	// up the repo by (prefix, slug) as before.
	effectivePrefix := in.Prefix
	// repoSlug is a local copy so we don't mutate the caller's IssueRepoInput.
	var repoSlug string
	if in.Repo != nil {
		repoSlug = in.Repo.RepoSlug
	}
	if in.Repo != nil && strings.TrimSpace(in.Repo.RemoteURL) != "" {
		resolved, regErr := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: in.Repo.RemoteURL,
			Actor:     in.Actor,
		})
		if regErr != nil {
			return Issue{}, fmt.Errorf("store: CreateIssue repo resolve: %w", regErr)
		}
		effectivePrefix = resolved.Prefix
		if strings.TrimSpace(repoSlug) == "" {
			repoSlug = resolved.Slug
		}
	}
	if strings.TrimSpace(effectivePrefix) == "" {
		return Issue{}, fmt.Errorf("store: CreateIssue: prefix is required (set Prefix or provide Repo.RemoteURL)")
	}

	labels := encodedLabels(in.Labels)
	typ := issueType(in.IssueType)

	for range idGenRetries {
		id, genErr := generateID(effectivePrefix)
		if genErr != nil {
			return Issue{}, fmt.Errorf("store: generate id: %w", genErr)
		}

		var repoTarget *model.RepoTarget
		now := clockNowUTC()
		issue := gormIssue{
			ID:          id,
			Prefix:      effectivePrefix,
			Title:       in.Title,
			Description: in.Description,
			Priority:    in.Priority,
			IssueType:   typ,
			State:       "open",
			Labels:      datatypes.JSON(labels),
			BranchName:  nullableStr(in.BranchName),
			URL:         nullableStr(in.URL),
			CreatedAt:   newGORMTime(now),
			UpdatedAt:   newGORMTime(now),
		}

		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&issue).Error; err != nil {
				return err
			}
			if in.Repo != nil {
				// Build a local copy so insertIssueRepoGORM gets the resolved slug
				// without mutating the caller's IssueRepoInput.
				repoIn := *in.Repo
				repoIn.RepoSlug = repoSlug
				var repoErr error
				repoTarget, repoErr = insertIssueRepoGORM(ctx, tx, id, effectivePrefix, repoIn)
				if repoErr != nil {
					return repoErr
				}
			}
			if in.Actor != "" {
				note := gormIssueNote{
					IssueID:   id,
					Actor:     nullableStr(in.Actor),
					Body:      fmt.Sprintf("created by %s", in.Actor),
					CreatedAt: newGORMTime(clockNowUTC()),
				}
				if err := tx.Create(&note).Error; err != nil {
					return fmt.Errorf("store: CreateIssue note: %w", err)
				}
			}
			return nil
		})

		if err == nil {
			return Issue{
				Issue: model.Issue{
					ID:          id,
					Identifier:  id,
					Title:       in.Title,
					Description: in.Description,
					Priority:    model.Priority(in.Priority + 1), // 0-indexed store → 1-indexed core
					State:       "open",
					Labels:      in.Labels,
					BranchName:  in.BranchName,
					URL:         in.URL,
					Repo:        repoTarget,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				IssueType: typ,
			}, nil
		}
		if !isPKConflict(err) {
			return Issue{}, fmt.Errorf("store: CreateIssue: %w", err)
		}
		// PK collision: retry with a fresh hash.
	}
	return Issue{}, fmt.Errorf("store: CreateIssue: id generation failed after %d retries", idGenRetries)
}

// GetIssueStatesByIDs returns the current state for each id present in the
// store. Missing IDs are silently absent from the result map — callers
// distinguish "not found" from "not requested" by map membership. This is a
// single batched query, not one call per ID.
func (s *Store) GetIssueStatesByIDs(ctx context.Context, ids []string) (map[string]model.IssueState, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	var rows []struct {
		ID    string
		State string
	}
	if len(ids) > 0 {
		err = db.WithContext(ctx).
			Model(&gormIssue{}).
			Select("id, state").
			Where("id IN ?", ids).
			Find(&rows).
			Error
		if err != nil {
			return nil, fmt.Errorf("store: GetIssueStatesByIDs: %w", err)
		}
	}

	result := make(map[string]model.IssueState, len(ids))
	for _, row := range rows {
		result[row.ID] = model.IssueState(row.State)
	}
	return result, nil
}

// GetIssue returns a single issue with its BlockedBy list populated.
// Returns ErrNotFound if no issue with that id exists.
func (s *Store) GetIssue(ctx context.Context, id string) (Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return Issue{}, err
	}

	var row gormIssue
	err = db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Issue{}, fmt.Errorf("store: %w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Issue{}, fmt.Errorf("store: GetIssue: %w", err)
	}

	iss := issueFromGORM(row)
	iss.BlockedBy, err = s.fetchBlockedBy(ctx, db, id)
	if err != nil {
		return Issue{}, err
	}
	issues := []Issue{iss}
	if err := s.populateIssueRepos(ctx, db, issues); err != nil {
		return Issue{}, err
	}
	iss = issues[0]
	return iss, nil
}

// ListFilter scopes a ListIssues / ReadyIssues call.
//
// Three-state prefix scoping:
//   - Prefix non-empty, AllRepos false  → WHERE prefix = Prefix  (default repo scope)
//   - Prefix empty,     AllRepos false  → WHERE prefix = ""      (matches nothing; explicit "no issues")
//   - AllRepos true                     → prefix filter omitted   (cross-repo / global listing)
type ListFilter struct {
	Prefix   string
	States   []model.IssueState // nil/empty = all states
	Limit    int                // 0 = no limit
	AllRepos bool               // true = omit the prefix WHERE clause entirely
}

// ListIssues returns issues matching the filter with BlockedBy populated.
func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	q := db.WithContext(ctx).
		Order("priority ASC").
		Order("created_at ASC")
	if !f.AllRepos {
		q = q.Where("prefix = ?", f.Prefix)
	}
	if len(f.States) > 0 {
		strs := make([]string, len(f.States))
		for i, st := range f.States {
			strs[i] = string(st)
		}
		q = q.Where("state IN ?", strs)
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	var rows []gormIssue
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: ListIssues: %w", err)
	}

	issues := issuesFromGORM(rows)

	if err := s.populateBlockedBy(ctx, db, issues); err != nil {
		return nil, err
	}
	if err := s.populateIssueRepos(ctx, db, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// ReadyIssues returns issues that are open (state in activeStates) and have
// all blockers in a terminal state. The terminal set is caller-supplied so
// the ready semantics match the operator's WorkspaceConfig.TerminalStates
// (never hardcoded to "closed").
//
// Only f.Prefix and f.AllRepos are consulted; f.States and f.Limit are
// ignored (the caller supplies active/terminal state sets explicitly, and
// result trimming is the caller's responsibility).
//
// Cross-prefix deps: an issue is only considered blocked by issues stored in
// the same configured database. Dangling edges (blocked_by_id references a
// deleted issue) are handled by ON DELETE CASCADE — if the blocker is deleted,
// the edge disappears and the child becomes unblocked automatically.
func (s *Store) ReadyIssues(ctx context.Context, f ListFilter, terminalStates []model.IssueState, activeStates []model.IssueState) ([]Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	if len(activeStates) == 0 {
		return []Issue{}, nil
	}

	active := make([]string, len(activeStates))
	for i, st := range activeStates {
		active[i] = string(st)
	}

	q := db.WithContext(ctx).
		Where("state IN ?", active)
	if !f.AllRepos {
		q = q.Where("prefix = ?", f.Prefix)
	}
	q = q.
		// Epics are organizational rollups, never a unit of dispatchable work.
		// This is purely issue_type-based and independent of whether the epic has
		// any members, so a childless epic is excluded too.
		Where("issue_type <> ?", "epic").
		Order("priority ASC").
		Order("created_at ASC")
	// Only blocking (dep_type='blocks') edges gate readiness. Membership edges
	// (parent-child, etc.) must never make a leaf or its epic non-ready.
	if len(terminalStates) == 0 {
		// When no states are terminal, every blocker is unsatisfied — so only
		// issues with no blocking edges at all are ready. Use the simpler
		// NOT EXISTS form to avoid the invalid "NOT IN ()" SQL syntax.
		q = q.Where("NOT EXISTS (SELECT 1 FROM bn_issue_deps d WHERE d.issue_id = bn_issues.id AND d.dep_type = ?)", DepTypeBlocks)
	} else {
		term := make([]string, len(terminalStates))
		for i, st := range terminalStates {
			term[i] = string(st)
		}
		q = q.Where(`
			NOT EXISTS (
			    SELECT 1 FROM bn_issue_deps d
			    JOIN bn_issues b ON b.id = d.blocked_by_id
			    WHERE d.issue_id = bn_issues.id
			      AND d.dep_type = ?
			      AND b.state NOT IN ?
			)`, DepTypeBlocks, term)
	}

	var rows []gormIssue
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: ReadyIssues: %w", err)
	}

	issues := issuesFromGORM(rows)

	if err := s.populateBlockedBy(ctx, db, issues); err != nil {
		return nil, err
	}
	if err := s.populateIssueRepos(ctx, db, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// UpdateIssueInput carries the optional fields to mutate. nil pointer = keep current.
type UpdateIssueInput struct {
	Title       *string
	Description *string
	Priority    *int
	State       *model.IssueState
	Labels      []string // nil = keep current; non-nil replaces (use []string{} to clear)
	BranchName  *string
	URL         *string
	Repo        *IssueRepoInput
	AppendNotes *AppendNotesInput
}

// AppendNotesInput appends a note to the issue without touching other fields.
type AppendNotesInput struct {
	Actor string
	Body  string
}

// UpdateIssue applies a partial update to an issue. Returns ErrNotFound if
// the issue does not exist. At least one field must be set.
func (s *Store) UpdateIssue(ctx context.Context, id string, in UpdateIssueInput) (Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return Issue{}, err
	}
	updates := map[string]any{}
	if in.Title != nil {
		updates["title"] = *in.Title
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if in.Priority != nil {
		updates["priority"] = *in.Priority
	}
	if in.State != nil {
		if !isValidIssueState(*in.State) {
			return Issue{}, fmt.Errorf("%w: %s", ErrInvalidIssueState, *in.State)
		}
		updates["state"] = string(*in.State)
	}
	if in.Labels != nil {
		updates["labels"] = datatypes.JSON(encodedLabels(in.Labels))
	}
	if in.BranchName != nil {
		updates["branch_name"] = nullableStr(*in.BranchName)
	}
	if in.URL != nil {
		updates["url"] = nullableStr(*in.URL)
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			updates["updated_at"] = newGORMTime(clockNowUTC())
			res := tx.Model(&gormIssue{}).Where("id = ?", id).Updates(updates)
			if res.Error != nil {
				return fmt.Errorf("store: UpdateIssue: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("store: %w: %s", ErrNotFound, id)
			}
		}

		var prefix string
		if in.Repo != nil || len(updates) == 0 {
			var issue gormIssue
			err := tx.Select("prefix").Where("id = ?", id).First(&issue).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("store: %w: %s", ErrNotFound, id)
			}
			if err != nil {
				return fmt.Errorf("store: UpdateIssue fetch prefix: %w", err)
			}
			prefix = issue.Prefix
		}

		if in.Repo != nil {
			if err := tx.Where("issue_id = ?", id).Delete(&gormIssueRepo{}).Error; err != nil {
				return fmt.Errorf("store: UpdateIssue repo delete: %w", err)
			}
			if _, err := insertIssueRepoGORM(ctx, tx, id, prefix, *in.Repo); err != nil {
				return err
			}
			if err := tx.Model(&gormIssue{}).
				Where("id = ?", id).
				Update("updated_at", newGORMTime(clockNowUTC())).
				Error; err != nil {
				return fmt.Errorf("store: UpdateIssue repo timestamp: %w", err)
			}
		}
		if in.AppendNotes != nil && strings.TrimSpace(in.AppendNotes.Body) != "" {
			note := gormIssueNote{
				IssueID:   id,
				Actor:     nullableStr(in.AppendNotes.Actor),
				Body:      in.AppendNotes.Body,
				CreatedAt: newGORMTime(clockNowUTC()),
			}
			if err := tx.Create(&note).Error; err != nil {
				return fmt.Errorf("store: UpdateIssue notes: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Issue{}, err
	}
	return s.GetIssue(ctx, id)
}

// CloseIssue sets an issue's state to "closed". Idempotent: closing an already-
// closed issue returns nil without inserting a duplicate note or bumping updated_at.
// If reason is non-empty it is appended as a note only when the state actually changes.
func (s *Store) CloseIssue(ctx context.Context, id, actor, reason string) error {
	db, err := s.p.gorm()
	if err != nil {
		return err
	}

	// Only update when the issue is not already closed so that repeated calls don't
	// accumulate noise notes or churn updated_at.
	res := db.WithContext(ctx).
		Model(&gormIssue{}).
		Where("id = ? AND state <> ?", id, "closed").
		Updates(map[string]any{"state": "closed", "updated_at": newGORMTime(clockNowUTC())})
	if res.Error != nil {
		return fmt.Errorf("store: CloseIssue: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// Either the issue does not exist, or it was already closed.
		exists, checkErr := s.issueExists(ctx, db, id)
		if checkErr != nil {
			return checkErr
		}
		if !exists {
			return fmt.Errorf("store: %w: %s", ErrNotFound, id)
		}
		// Already closed — idempotent, skip note insertion.
		return nil
	}

	if strings.TrimSpace(reason) != "" {
		note := gormIssueNote{
			IssueID:   id,
			Actor:     nullableStr(actor),
			Body:      strings.TrimSpace(reason),
			CreatedAt: newGORMTime(clockNowUTC()),
		}
		if err := db.WithContext(ctx).Create(&note).Error; err != nil {
			return fmt.Errorf("store: CloseIssue note: %w", err)
		}
	}
	return nil
}

// DeleteIssue hard-deletes an issue and its dependent edges/notes via CASCADE.
// Returns ErrNotFound if the issue does not exist.
func (s *Store) DeleteIssue(ctx context.Context, id string) error {
	db, err := s.p.gorm()
	if err != nil {
		return err
	}

	res := db.WithContext(ctx).Where("id = ?", id).Delete(&gormIssue{})
	if res.Error != nil {
		return fmt.Errorf("store: DeleteIssue: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: %w: %s", ErrNotFound, id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dependency management
// ---------------------------------------------------------------------------

// Dependency edge kinds. Only DepTypeBlocks gates readiness and cycle
// detection; every other kind (DepTypeParentChild and any custom value) is a
// non-blocking membership/association edge. This is a deliberate divergence
// from bd, whose AffectsReadyWork() treats parent-child as blocking: the
// planning skills require a leaf to be ready without depending on its epic.
const (
	DepTypeBlocks      = "blocks"
	DepTypeParentChild = "parent-child"
)

// maxDepTypeLen bounds a dependency type string, matching bd's IsValid rule.
const maxDepTypeLen = 50

// AddDep adds a blocking edge: childID is blocked until parentID reaches a
// terminal state. It is a thin wrapper over AddTypedDep with DepTypeBlocks,
// preserving the existing call surface.
func (s *Store) AddDep(ctx context.Context, childID, parentID string) error {
	return s.AddTypedDep(ctx, childID, parentID, DepTypeBlocks)
}

// AddTypedDep adds a dependency edge of the given kind. For DepTypeBlocks the
// edge means "childID is blocked until parentID reaches a terminal state" and
// the insert is guarded against cycles inside a single serializable
// transaction. For non-blocking kinds (parent-child, etc.) the edge is pure
// membership: the reachability cycle check is skipped because such edges never
// participate in ready/cycle computation. Self-edges are rejected for all
// kinds. Returns ErrCycle on a blocking cycle, ErrDuplicateDep if the (child,
// parent) pair already exists, or ErrNotFound if either issue is missing.
//
// One-edge-per-pair: the primary key is (issue_id, blocked_by_id) and does NOT
// include dep_type, so at most one edge of any kind can exist between an
// ordered pair — a second AddTypedDep for the same pair returns ErrDuplicateDep
// regardless of type (first edge wins). This is intentional for the two-level
// epic→leaf model where a leaf belongs to its epic but never also blocks on it;
// dual blocks+membership edges between the same pair are out of scope.
func (s *Store) AddTypedDep(ctx context.Context, childID, parentID, depType string) error {
	if childID == parentID {
		return fmt.Errorf("store: %w: %s → %s", ErrCycle, childID, parentID)
	}
	depType = normalizeDepType(depType)
	db, err := s.p.gorm()
	if err != nil {
		return err
	}

	blocking := depType == DepTypeBlocks

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDepGraphGuard(tx); err != nil {
			return err
		}
		for _, id := range []string{childID, parentID} {
			exists, err := s.issueExists(ctx, tx, id)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("store: %w: %s", ErrNotFound, id)
			}
		}
		if blocking {
			graph, err := loadDepGraph(tx)
			if err != nil {
				return err
			}
			if reaches(graph, parentID, childID) {
				return fmt.Errorf("store: %w: %s → %s", ErrCycle, childID, parentID)
			}
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&gormIssueDep{IssueID: childID, BlockedByID: parentID, DepType: depType})
		if res.Error != nil {
			if isDupKeyConflict(res.Error) {
				return ErrDuplicateDep
			}
			return fmt.Errorf("store: AddDep: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrDuplicateDep
		}
		return nil
	})
}

// RemoveDep removes a dependency edge. Returns ErrNotFound if the edge does
// not exist.
func (s *Store) RemoveDep(ctx context.Context, childID, parentID string) error {
	db, err := s.p.gorm()
	if err != nil {
		return err
	}

	res := db.WithContext(ctx).
		Where("issue_id = ? AND blocked_by_id = ?", childID, parentID).
		Delete(&gormIssueDep{})
	if res.Error != nil {
		return fmt.Errorf("store: RemoveDep: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: %w: dep %s → %s", ErrNotFound, childID, parentID)
	}
	return nil
}

// DepEdge is one directed dependency edge returned by ListDeps. DepType
// distinguishes blocking ("blocks") edges from membership ("parent-child", …)
// edges.
type DepEdge struct {
	IssueID     string
	BlockedByID string
	DepType     string
}

// ListDeps returns all dependency edges (every kind) for issues matching the
// filter, ordered deterministically for stable export/tree output.
//
// f.AllRepos=true omits the prefix clause, returning edges across all projects.
// Under topology (a) this is equivalent to cross-repo scope.
func (s *Store) ListDeps(ctx context.Context, f ListFilter) ([]DepEdge, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	q := db.WithContext(ctx).
		Table("bn_issue_deps AS d").
		Select("d.issue_id, d.blocked_by_id, d.dep_type").
		Joins("JOIN bn_issues i ON i.id = d.issue_id")
	if !f.AllRepos {
		q = q.Where("i.prefix = ?", f.Prefix)
	}
	q = q.Order("d.issue_id ASC, d.blocked_by_id ASC, d.dep_type ASC")

	var edges []DepEdge
	if err := q.Scan(&edges).Error; err != nil {
		return nil, fmt.Errorf("store: ListDeps: %w", err)
	}
	return edges, nil
}

// ListBlockingDeps returns only the blocking (dep_type='blocks') edges for
// issues matching the filter, filtered in SQL rather than in Go. The ordering
// views (dep tree, dep cycles) reason solely about blocking edges, so this
// avoids fetching and discarding membership rows.
//
// f.AllRepos=true omits the prefix clause.
func (s *Store) ListBlockingDeps(ctx context.Context, f ListFilter) ([]DepEdge, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	q := db.WithContext(ctx).
		Table("bn_issue_deps AS d").
		Select("d.issue_id, d.blocked_by_id, d.dep_type").
		Joins("JOIN bn_issues i ON i.id = d.issue_id")
	if !f.AllRepos {
		q = q.Where("i.prefix = ?", f.Prefix)
	}
	q = q.Where("d.dep_type = ?", DepTypeBlocks).
		Order("d.issue_id ASC, d.blocked_by_id ASC")

	var edges []DepEdge
	if err := q.Scan(&edges).Error; err != nil {
		return nil, fmt.Errorf("store: ListBlockingDeps: %w", err)
	}
	return edges, nil
}

// ListMembers returns the issues that are parent-child members (children) of
// parentID — i.e. rows where blocked_by_id=parentID and dep_type='parent-child'.
// Ordered by priority then creation time. Backs `bn list --epic`. Membership is
// non-blocking, so this is the authoritative way to assert "every epic has ≥2
// children" without raw SQL. A prefix-scoped query (AllRepos=false) guarantees
// isolation — a foreign epic ID cannot leak another project's children.
//
// f.AllRepos=true omits the prefix clause, returning members across all projects.
// f.States and f.Limit are not consulted.
func (s *Store) ListMembers(ctx context.Context, f ListFilter, parentID string) ([]Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	q := db.WithContext(ctx).
		Table("bn_issues AS i").
		Joins("JOIN bn_issue_deps d ON d.issue_id = i.id").
		Where("d.blocked_by_id = ? AND d.dep_type = ?", parentID, DepTypeParentChild)
	if !f.AllRepos {
		q = q.Where("i.prefix = ?", f.Prefix)
	}
	q = q.Order("i.priority ASC").Order("i.created_at ASC")

	var rows []gormIssue
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: ListMembers: %w", err)
	}

	issues := issuesFromGORM(rows)
	if err := s.populateBlockedBy(ctx, db, issues); err != nil {
		return nil, err
	}
	if err := s.populateIssueRepos(ctx, db, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// ListParents returns the parent/epic issues that childID is a parent-child
// member of — i.e. rows where issue_id=childID and dep_type='parent-child',
// joined to the parent on blocked_by_id. Inverse of ListMembers; gives a leaf
// a read surface back to its epic (used by `bn show`). Prefix-scoped by default
// for the same isolation reason as ListMembers.
//
// f.AllRepos=true omits the prefix clause. f.States and f.Limit are not consulted.
func (s *Store) ListParents(ctx context.Context, f ListFilter, childID string) ([]Issue, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}

	q := db.WithContext(ctx).
		Table("bn_issues AS i").
		Joins("JOIN bn_issue_deps d ON d.blocked_by_id = i.id").
		Where("d.issue_id = ? AND d.dep_type = ?", childID, DepTypeParentChild)
	if !f.AllRepos {
		q = q.Where("i.prefix = ?", f.Prefix)
	}
	q = q.Order("i.priority ASC").Order("i.created_at ASC")

	var rows []gormIssue
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: ListParents: %w", err)
	}
	return issuesFromGORM(rows), nil
}

// ---------------------------------------------------------------------------
// Import (create-only, never-regress-terminal)
// ---------------------------------------------------------------------------

// ImportInput carries one issue from a bd-export JSONL for seeding.
type ImportInput struct {
	ID          string
	Prefix      string
	Title       string
	Description string
	Priority    int
	IssueType   string
	State       string
	Labels      []string
	BranchName  string
	URL         string
	Deps        []string // blocked_by ids (dep_type='blocks')
	ParentEdges []string // parent/epic ids this issue is a parent-child member of
}

// ImportIssues loads a batch of issues into the store using merge semantics.
// Deprecated: use ImportIssuesFull with ImportModeMerge instead. This function
// is retained for test compatibility only; production callers use ImportIssuesFull.
//
// Semantics:
//   - If the issue does not exist: insert it.
//   - If the issue exists and its current state is NOT terminal: update all
//     non-state fields; the state field is updated if the import state is
//     NOT terminal.
//   - If the issue exists and its current state IS terminal: do NOT regress
//     the state (the orchestrator may have closed it); update other fields.
//
// Deps are inserted in a second pass (all issues first, then edges) to avoid
// FK forward-reference failures. The entire batch runs in one transaction.
func (s *Store) ImportIssues(ctx context.Context, items []ImportInput, terminalStates []model.IssueState) error {
	_, err := s.ImportIssuesFull(ctx, items, ImportOptions{
		TerminalStates:          terminalStates,
		Mode:                    ImportModeMerge,
		PreserveTerminalImports: true,
	})
	if err != nil {
		return fmt.Errorf("store: ImportIssues: %w", err)
	}
	return nil
}

// ImportMode controls how ImportIssuesFull handles existing issues.
type ImportMode int

const (
	// ImportModeCreateOnly inserts new issues and skips existing ones.
	// Safe for idempotent seeding; never modifies state the orchestrator owns.
	ImportModeCreateOnly ImportMode = iota
	// ImportModeMerge updates non-state fields for existing issues but never
	// regresses a terminal state. Same semantics as the original ImportIssues.
	ImportModeMerge
)

// ImportOptions configures ImportIssuesFull.
type ImportOptions struct {
	TerminalStates          []model.IssueState
	Mode                    ImportMode
	PreserveTerminalImports bool
}

// ImportResult counts what ImportIssuesFull did.
type ImportResult struct {
	Created                   int // issues inserted for the first time
	Updated                   int // issues that already existed (merge mode only)
	Skipped                   int // issues skipped because they already existed (create-only)
	CrossPrefixConflicts      int // issue IDs already owned by another prefix
	DepsAdded                 int // blocking dep edges successfully inserted
	DepsSkippedMissingBlocker int // blocking dep edges skipped — blocker not in destination prefix
	DepsSkippedDuplicate      int // dep edges skipped because they already existed (any kind)
	DepsSkippedSelf           int // dep edges skipped because issue_id == blocked_by_id (any kind)
	DepsSkippedCycle          int // blocking dep edges skipped because they would create a cycle
	ParentEdgesAdded          int // parent-child membership edges successfully inserted
	ParentEdgesSkippedMissing int // parent-child edges skipped — parent not in destination prefix
}

// ImportIssuesFull loads a batch of issues with configurable mode and returns
// counts. It resolves B1 (dangling-dep FK abort): pass-2 only inserts edges
// whose blocked_by_id exists in the destination prefix.
//
// Deps are inserted in a second pass (all issues first, then edges) to avoid
// FK forward-reference failures. The entire batch runs in one transaction.
func (s *Store) ImportIssuesFull(ctx context.Context, items []ImportInput, opts ImportOptions) (ImportResult, error) {
	if len(items) == 0 {
		return ImportResult{}, nil
	}
	items = dedupeImportInputs(items)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.importIssuesFullOnce(ctx, items, opts)
		if err == nil {
			return result, nil
		}
		if !isSerializationFailure(err) {
			return ImportResult{}, err
		}
		lastErr = err
	}
	return ImportResult{}, lastErr
}

func (s *Store) importIssuesFullOnce(ctx context.Context, items []ImportInput, opts ImportOptions) (ImportResult, error) {
	db, err := s.p.gorm()
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		written := make(map[string]bool, len(items))
		terminalSet := terminalStateSet(opts.TerminalStates)

		for _, item := range items {
			existing, alreadyExists, err := s.getImportIssueSnapshot(ctx, tx, item.ID)
			if err != nil {
				return err
			}
			if alreadyExists && existing.Prefix != item.Prefix {
				result.CrossPrefixConflicts++
				continue
			}

			if opts.Mode == ImportModeCreateOnly {
				if alreadyExists {
					result.Skipped++
					continue
				}
				issue := gormIssueFromImport(item, clockNowUTC())
				res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&issue)
				if res.Error != nil {
					return fmt.Errorf("store: ImportIssuesFull create %s: %w", item.ID, res.Error)
				}
				if res.RowsAffected > 0 {
					result.Created++
					written[item.ID] = true
				} else {
					result.Skipped++
				}
				continue
			}

			if !alreadyExists {
				issue := gormIssueFromImport(item, clockNowUTC())
				if err := tx.Create(&issue).Error; err != nil {
					return fmt.Errorf("store: ImportIssuesFull merge %s: %w", item.ID, err)
				}
				result.Created++
				written[item.ID] = true
				continue
			}

			keepState := terminalSet[existing.State] || (opts.PreserveTerminalImports && terminalSet[model.IssueState(item.State)])
			updates := importIssueUpdateMap(item, keepState)
			res := tx.Model(&gormIssue{}).Where("id = ?", item.ID).Updates(updates)
			if res.Error != nil {
				return fmt.Errorf("store: ImportIssuesFull merge %s: %w", item.ID, res.Error)
			}
			result.Updated++
			written[item.ID] = true
		}

		if err := lockDepGraphGuard(tx); err != nil {
			return err
		}
		graph, err := loadDepGraph(tx)
		if err != nil {
			return err
		}

		for _, item := range items {
			if !written[item.ID] {
				continue
			}
			seenDeps := make(map[string]bool, len(item.Deps))
			for _, blockerID := range item.Deps {
				if blockerID == item.ID {
					result.DepsSkippedSelf++
					continue
				}
				if seenDeps[blockerID] {
					result.DepsSkippedDuplicate++
					continue
				}
				seenDeps[blockerID] = true
				if exists, err := s.issueExistsInPrefix(ctx, tx, blockerID, item.Prefix); err != nil {
					return err
				} else if !exists {
					result.DepsSkippedMissingBlocker++
					continue
				}
				if reaches(graph, blockerID, item.ID) {
					result.DepsSkippedCycle++
					continue
				}
				res := tx.Clauses(clause.OnConflict{DoNothing: true}).
					Create(&gormIssueDep{IssueID: item.ID, BlockedByID: blockerID, DepType: DepTypeBlocks})
				if res.Error != nil {
					return fmt.Errorf("store: ImportIssuesFull dep %s→%s: %w", item.ID, blockerID, res.Error)
				}
				if res.RowsAffected > 0 {
					result.DepsAdded++
					graph[item.ID] = append(graph[item.ID], blockerID)
				} else {
					result.DepsSkippedDuplicate++
				}
			}

			// Parent-child membership edges: non-blocking, so no cycle guard and
			// no contribution to the blocking graph. A missing parent and a
			// successful insert use dedicated counters (ParentEdges*) so a dropped
			// hierarchy edge is never reported as a broken *blocker*; self and
			// duplicate are kind-neutral and share the existing buckets.
			seenParents := make(map[string]bool, len(item.ParentEdges))
			for _, parentID := range item.ParentEdges {
				if parentID == item.ID {
					result.DepsSkippedSelf++
					continue
				}
				if seenParents[parentID] {
					result.DepsSkippedDuplicate++
					continue
				}
				seenParents[parentID] = true
				if exists, err := s.issueExistsInPrefix(ctx, tx, parentID, item.Prefix); err != nil {
					return err
				} else if !exists {
					result.ParentEdgesSkippedMissing++
					continue
				}
				// blocked_by_id stores the PARENT id for parent-child rows (the
				// column is overloaded across edge kinds), so the same gormIssueDep
				// shape carries both blocking and membership edges.
				res := tx.Clauses(clause.OnConflict{DoNothing: true}).
					Create(&gormIssueDep{IssueID: item.ID, BlockedByID: parentID, DepType: DepTypeParentChild})
				if res.Error != nil {
					return fmt.Errorf("store: ImportIssuesFull parent-child %s→%s: %w", item.ID, parentID, res.Error)
				}
				if res.RowsAffected > 0 {
					result.ParentEdgesAdded++
				} else {
					result.DepsSkippedDuplicate++
				}
			}
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Memory (bn remember / bn memories) — requires migration 0002_bn_memories.sql
// ---------------------------------------------------------------------------

// MemoryInput carries the fields for a new memory row.
type MemoryInput struct {
	Prefix string // empty string → global (stored as NULL)
	Body   string
	Type   string // free-text: user|feedback|project|reference
	Tags   []string
}

// Memory is one row from bn_memories.
type Memory struct {
	ID        int64
	Prefix    *string // nil = global
	Body      string
	Type      *string // nil when not set
	Tags      []string
	CreatedAt time.Time
}

// MemoryFilter scopes a SearchMemories call.
type MemoryFilter struct {
	Prefix string   // empty = active project + globals; applies when All=false
	All    bool     // true = every project (ignore Prefix)
	Type   string   // filter by mtype; empty = no filter
	Tags   []string // all tags must be present; nil = no filter
	Limit  int      // 0 = default 50
}

const defaultMemoryLimit = 50

const maxMemoryTagLength = 255

// InsertMemory adds a new memory row. An empty Prefix stores NULL (global).
// For non-global memories, the prefix must be registered in bn_projects.
func (s *Store) InsertMemory(ctx context.Context, in MemoryInput) (Memory, error) {
	db, err := s.p.gorm()
	if err != nil {
		return Memory{}, err
	}
	tags, err := normalizeMemoryTags(in.Tags)
	if err != nil {
		return Memory{}, fmt.Errorf("store: InsertMemory tags: %w", err)
	}

	var prefix *string
	if in.Prefix != "" {
		prefix = &in.Prefix
	}
	var mtype *string
	if in.Type != "" {
		mtype = &in.Type
	}

	memory := gormMemory{
		Prefix:    prefix,
		Body:      in.Body,
		MType:     mtype,
		Tags:      datatypes.JSON(encodedLabels(tags)),
		CreatedAt: newGORMTime(clockNowUTC()),
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&memory).Error; err != nil {
			return err
		}
		for _, tag := range tags {
			if err := tx.Create(&gormMemoryTag{MemoryID: memory.ID, Tag: tag}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Memory{}, fmt.Errorf("store: InsertMemory: %w", err)
	}

	return Memory{
		ID:        memory.ID,
		Prefix:    prefix,
		Body:      in.Body,
		Type:      mtype,
		Tags:      tags,
		CreatedAt: memory.CreatedAt.UTC(),
	}, nil
}

// SearchMemories returns memories matching the filter. When query is non-empty,
// full-text search is used (plainto_tsquery for safe user input). When empty,
// recent memories are returned ordered by created_at DESC.
// Scope: when All=false, returns rows for Prefix + global (prefix IS NULL).
func (s *Store) SearchMemories(ctx context.Context, query string, f MemoryFilter) ([]Memory, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)

	limit := f.Limit
	if limit <= 0 {
		limit = defaultMemoryLimit
	}

	q := db.WithContext(ctx).Model(&gormMemory{})
	tags, err := normalizeMemoryTags(f.Tags)
	if err != nil {
		return nil, fmt.Errorf("store: SearchMemories tags: %w", err)
	}
	if !f.All {
		// Scope to active project + globals.
		pfx := f.Prefix
		q = q.Where("prefix = ? OR prefix IS NULL", pfx)
	}

	q = applyMemorySearch(q, s.p.driver, query)

	if f.Type != "" {
		q = q.Where("mtype = ?", f.Type)
	}

	if len(tags) > 0 {
		q = q.Where(
			`id IN (
				SELECT memory_id
				FROM bn_memory_tags
				WHERE tag IN ?
				GROUP BY memory_id
				HAVING COUNT(DISTINCT tag) = ?
			)`,
			tags, len(tags),
		)
	}

	var rows []gormMemory
	err = q.Order("created_at DESC").Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: SearchMemories: %w", err)
	}

	memories := make([]Memory, 0, len(rows))
	for _, row := range rows {
		memories = append(memories, memoryFromGORM(row))
	}
	return memories, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Store) issueExists(ctx context.Context, db *gorm.DB, id string) (bool, error) {
	var count int64
	err := db.WithContext(ctx).Model(&gormIssue{}).Where("id = ?", id).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("store: issueExists: %w", err)
	}
	return count > 0, nil
}

func (s *Store) issueExistsInPrefix(ctx context.Context, db *gorm.DB, id, prefix string) (bool, error) {
	var count int64
	err := db.WithContext(ctx).Model(&gormIssue{}).Where("id = ? AND prefix = ?", id, prefix).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("store: issueExistsInPrefix: %w", err)
	}
	return count > 0, nil
}

type importIssueSnapshot struct {
	Prefix string
	State  model.IssueState
}

func (s *Store) getImportIssueSnapshot(ctx context.Context, db *gorm.DB, id string) (importIssueSnapshot, bool, error) {
	var issue gormIssue
	res := db.WithContext(ctx).Select("prefix", "state").Where("id = ?", id).Find(&issue)
	if res.Error != nil {
		return importIssueSnapshot{}, false, fmt.Errorf("store: getImportIssueSnapshot: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return importIssueSnapshot{}, false, nil
	}
	return importIssueSnapshot{Prefix: issue.Prefix, State: model.IssueState(issue.State)}, true, nil
}

func (s *Store) fetchBlockedBy(ctx context.Context, db *gorm.DB, id string) ([]string, error) {
	var deps []gormIssueDep
	err := db.WithContext(ctx).
		Where("issue_id = ? AND dep_type = ?", id, DepTypeBlocks).
		Order("blocked_by_id ASC").
		Find(&deps).
		Error
	if err != nil {
		return nil, fmt.Errorf("store: fetchBlockedBy: %w", err)
	}
	ids := make([]string, 0, len(deps))
	for _, dep := range deps {
		ids = append(ids, dep.BlockedByID)
	}
	return ids, nil
}

func (s *Store) populateBlockedBy(ctx context.Context, db *gorm.DB, issues []Issue) error {
	if len(issues) == 0 {
		return nil
	}

	ids := make([]string, len(issues))
	idxByID := make(map[string]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		idxByID[iss.ID] = i
	}

	var deps []gormIssueDep
	err := db.WithContext(ctx).
		Where("issue_id IN ? AND dep_type = ?", ids, DepTypeBlocks).
		Order("issue_id ASC, blocked_by_id ASC").
		Find(&deps).
		Error
	if err != nil {
		return fmt.Errorf("store: populateBlockedBy: %w", err)
	}

	for _, dep := range deps {
		if idx, ok := idxByID[dep.IssueID]; ok {
			issues[idx].BlockedBy = append(issues[idx].BlockedBy, dep.BlockedByID)
		}
	}
	return nil
}

func insertIssueRepoGORM(ctx context.Context, tx *gorm.DB, issueID, prefix string, in IssueRepoInput) (*model.RepoTarget, error) {
	repo, err := getRepoBySlugGORM(ctx, tx, prefix, in.RepoSlug)
	if err != nil {
		return nil, err
	}
	if !repo.Enabled {
		return nil, fmt.Errorf("store: %w: repo %s", ErrDisabled, repo.Slug)
	}

	requestedRef := strings.TrimSpace(in.RequestedRef)
	if err := repovalidation.ValidateDefaultBranch(requestedRef); err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo requested_ref: %w", err)
	}
	baseRef := strings.TrimSpace(in.BaseRef)
	if baseRef == "" {
		baseRef = repo.DefaultBranch
	}
	if err := repovalidation.ValidateDefaultBranch(baseRef); err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo base_ref: %w", err)
	}
	workBranch := strings.TrimSpace(in.WorkBranch)
	if err := repovalidation.ValidateDefaultBranch(workBranch); err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo work_branch: %w", err)
	}
	worktreeSubdir := strings.TrimSpace(in.WorktreeSubdir)
	if worktreeSubdir == "" {
		worktreeSubdir = repo.WorktreeSubdir
	}
	if err := repovalidation.ValidateWorktreeSubdir(worktreeSubdir); err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo worktree_subdir: %w", err)
	}
	metadata, err := encodeJSONObject(in.Metadata)
	if err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo metadata: %w", err)
	}
	now := clockNowUTC()

	link := gormIssueRepo{
		IssueID:        issueID,
		RepoID:         repo.ID,
		RequestedRef:   requestedRef,
		BaseRef:        baseRef,
		WorkBranch:     workBranch,
		WorktreeSubdir: worktreeSubdir,
		Metadata:       datatypes.JSON(metadata),
		CreatedAt:      newGORMTime(now),
		UpdatedAt:      newGORMTime(now),
	}
	if err := tx.WithContext(ctx).Create(&link).Error; err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo: %w", err)
	}

	return repoTargetFromIssueRepo(repo, requestedRef, baseRef, workBranch, worktreeSubdir, in.Metadata), nil
}

func (s *Store) populateIssueRepos(ctx context.Context, db *gorm.DB, issues []Issue) error {
	if len(issues) == 0 {
		return nil
	}

	ids := make([]string, len(issues))
	idxByID := make(map[string]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		idxByID[iss.ID] = i
	}

	var rows []struct {
		IssueID        string
		ID             string
		Slug           string
		RemoteURL      string
		DefaultBranch  string
		RequestedRef   string
		BaseRef        string
		WorkBranch     string
		WorktreeSubdir string
		CloneStrategy  string
		AuthRef        string
		Metadata       datatypes.JSON
	}
	err := db.WithContext(ctx).
		Table("bn_issue_repos AS ir").
		Select(`ir.issue_id,
			r.id, r.slug, r.remote_url, r.default_branch,
			ir.requested_ref, ir.base_ref, ir.work_branch,
			CASE WHEN ir.worktree_subdir = '' THEN r.worktree_subdir ELSE ir.worktree_subdir END AS worktree_subdir,
			r.clone_strategy, r.auth_ref, ir.metadata`).
		Joins("JOIN bn_repos r ON r.id = ir.repo_id").
		Where("ir.issue_id IN ?", ids).
		Scan(&rows).
		Error
	if err != nil {
		return fmt.Errorf("store: populateIssueRepos: %w", err)
	}

	for _, row := range rows {
		target := model.RepoTarget{
			ID:             row.ID,
			Slug:           row.Slug,
			RemoteURL:      row.RemoteURL,
			DefaultBranch:  row.DefaultBranch,
			RequestedRef:   row.RequestedRef,
			BaseRef:        row.BaseRef,
			WorkBranch:     row.WorkBranch,
			WorktreeSubdir: row.WorktreeSubdir,
			CloneStrategy:  row.CloneStrategy,
			AuthRef:        row.AuthRef,
			Metadata:       decodeJSONObject(row.Metadata),
		}
		if idx, ok := idxByID[row.IssueID]; ok {
			issues[idx].Repo = &target
		}
	}
	return nil
}

func repoTargetFromIssueRepo(repo Repo, requestedRef, baseRef, workBranch, worktreeSubdir string, metadata map[string]any) *model.RepoTarget {
	if metadata == nil {
		metadata = map[string]any{}
	}
	return &model.RepoTarget{
		ID:             repo.ID,
		Slug:           repo.Slug,
		RemoteURL:      repo.RemoteURL,
		DefaultBranch:  repo.DefaultBranch,
		RequestedRef:   requestedRef,
		BaseRef:        baseRef,
		WorkBranch:     workBranch,
		WorktreeSubdir: worktreeSubdir,
		CloneStrategy:  repo.CloneStrategy,
		AuthRef:        repo.AuthRef,
		Metadata:       metadata,
	}
}

func issueFromGORM(row gormIssue) Issue {
	return Issue{
		Issue: model.Issue{
			ID:          row.ID,
			Identifier:  derefStr(row.Identifier, row.ID),
			Title:       row.Title,
			Description: row.Description,
			Priority:    storePriorityToCore(row.Priority),
			State:       model.IssueState(row.State),
			Labels:      decodeLabels(row.Labels),
			BranchName:  derefStr(row.BranchName, ""),
			URL:         derefStr(row.URL, ""),
			CreatedAt:   row.CreatedAt.UTC(),
			UpdatedAt:   row.UpdatedAt.UTC(),
		},
		IssueType: row.IssueType,
	}
}

func issuesFromGORM(rows []gormIssue) []Issue {
	issues := make([]Issue, 0, len(rows))
	for _, row := range rows {
		issues = append(issues, issueFromGORM(row))
	}
	return issues
}

func memoryFromGORM(row gormMemory) Memory {
	return Memory{
		ID:        row.ID,
		Prefix:    row.Prefix,
		Body:      row.Body,
		Type:      row.MType,
		Tags:      decodeLabels(row.Tags),
		CreatedAt: row.CreatedAt.UTC(),
	}
}

func applyMemorySearch(q *gorm.DB, driver Driver, query string) *gorm.DB {
	if query == "" {
		return q
	}
	switch driver {
	case DriverPostgres:
		return q.
			Where("tsv @@ plainto_tsquery('english', ?)", query).
			Order(clause.OrderBy{
				Expression: clause.Expr{
					SQL:  "ts_rank(tsv, plainto_tsquery('english', ?)) DESC",
					Vars: []any{query},
				},
			})
	case DriverMySQL:
		return q.
			Where("MATCH(body) AGAINST (? IN NATURAL LANGUAGE MODE)", query).
			Order(clause.OrderBy{
				Expression: clause.Expr{
					SQL:  "MATCH(body) AGAINST (? IN NATURAL LANGUAGE MODE) DESC",
					Vars: []any{query},
				},
			})
	case DriverSQLite:
		ftsQuery := sqliteFTSQuery(query)
		if ftsQuery == "" {
			return q.Where("1 = 0")
		}
		return q.
			Joins("JOIN bn_memories_fts ON bn_memories_fts.rowid = bn_memories.id").
			Where("bn_memories_fts MATCH ?", ftsQuery).
			Order("bm25(bn_memories_fts) ASC")
	default:
		return applyLikeMemorySearch(q, query)
	}
}

func applyLikeMemorySearch(q *gorm.DB, query string) *gorm.DB {
	for _, term := range strings.Fields(query) {
		q = q.Where(`body LIKE ? ESCAPE '\'`, "%"+escapeLike(term)+"%")
	}
	return q
}

func sqliteFTSQuery(query string) string {
	terms := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		if term == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}

func normalizeMemoryTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" {
			return nil, fmt.Errorf("memory tag must not be empty")
		}
		if utf8.RuneCountInString(tag) > maxMemoryTagLength {
			return nil, fmt.Errorf("memory tag %q exceeds %d characters", tag, maxMemoryTagLength)
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}

func getRepoBySlugGORM(ctx context.Context, db *gorm.DB, prefix, slug string) (Repo, error) {
	var row gormRepo
	err := db.WithContext(ctx).
		Where("prefix = ? AND slug = ?", prefix, slug).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Repo{}, fmt.Errorf("store: %w: repo %s", ErrNotFound, slug)
	}
	if err != nil {
		return Repo{}, fmt.Errorf("store: GetRepoBySlug: %w", err)
	}
	return repoFromGORM(row), nil
}

func getRepoByRemoteURLGORM(ctx context.Context, db *gorm.DB, normalizedURL string) (Repo, error) {
	var row gormRepo
	err := db.WithContext(ctx).
		Where("remote_url = ?", normalizedURL).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Repo{}, fmt.Errorf("store: %w: no repo with remote URL %s", ErrNotFound, normalizedURL)
	}
	if err != nil {
		return Repo{}, fmt.Errorf("store: GetRepoByRemoteURL: %w", err)
	}
	return repoFromGORM(row), nil
}

func repoFromGORM(row gormRepo) Repo {
	return Repo{
		ID:             row.ID,
		Prefix:         row.Prefix,
		Slug:           row.Slug,
		DisplayName:    row.DisplayName,
		RemoteURL:      row.RemoteURL,
		DefaultBranch:  row.DefaultBranch,
		WorktreeSubdir: row.WorktreeSubdir,
		CloneStrategy:  row.CloneStrategy,
		AuthRef:        row.AuthRef,
		Enabled:        row.Enabled,
		Metadata:       decodeJSONObject(row.Metadata),
		CreatedAt:      row.CreatedAt.UTC(),
		UpdatedAt:      row.UpdatedAt.UTC(),
		CreatedBy:      row.CreatedBy,
		UpdatedBy:      row.UpdatedBy,
	}
}

func gormIssueFromImport(item ImportInput, now time.Time) gormIssue {
	return gormIssue{
		ID:          item.ID,
		Prefix:      item.Prefix,
		Title:       item.Title,
		Description: item.Description,
		Priority:    item.Priority,
		IssueType:   issueType(item.IssueType),
		State:       item.State,
		Labels:      datatypes.JSON(encodedLabels(item.Labels)),
		BranchName:  nullableStr(item.BranchName),
		URL:         nullableStr(item.URL),
		CreatedAt:   newGORMTime(now),
		UpdatedAt:   newGORMTime(now),
	}
}

func importIssueUpdateMap(item ImportInput, keepState bool) map[string]any {
	updates := map[string]any{
		"title":       item.Title,
		"description": item.Description,
		"priority":    item.Priority,
		"issue_type":  issueType(item.IssueType),
		"labels":      datatypes.JSON(encodedLabels(item.Labels)),
		"branch_name": nullableStr(item.BranchName),
		"url":         nullableStr(item.URL),
		"updated_at":  newGORMTime(clockNowUTC()),
	}
	if !keepState {
		updates["state"] = item.State
	}
	return updates
}

func terminalStateSet(states []model.IssueState) map[model.IssueState]bool {
	out := make(map[model.IssueState]bool, len(states))
	for _, state := range states {
		out[state] = true
	}
	return out
}

func isValidIssueState(state model.IssueState) bool {
	switch state {
	case "open", "in_progress", "blocked", "closed", "done":
		return true
	default:
		return false
	}
}

func lockDepGraphGuard(tx *gorm.DB) error {
	res := tx.Model(&gormDepGraphGuard{}).
		Where("id = ?", int16(1)).
		Update("updated_at", newGORMTime(clockNowUTC()))
	if res.Error != nil {
		return fmt.Errorf("store: dependency graph guard: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: dependency graph guard missing")
	}
	return nil
}

// normalizeDepType trims a dependency type and defaults the empty string to
// DepTypeBlocks so callers (CLI, import) can pass through user input safely.
func normalizeDepType(depType string) string {
	depType = strings.TrimSpace(depType)
	if depType == "" {
		return DepTypeBlocks
	}
	return depType
}

// ValidateDepType applies the permissive bd-compatible rule: any non-empty type
// up to maxDepTypeLen characters is accepted (so scripts copied from bd
// workflows using related/discovered-from/etc. do not hard-error). Returns the
// normalized value or an error a CLI can surface in place of a cobra flag error.
func ValidateDepType(depType string) (string, error) {
	normalized := normalizeDepType(depType)
	if len(normalized) > maxDepTypeLen {
		return "", fmt.Errorf("invalid dependency type %q: must be non-empty and at most %d characters", depType, maxDepTypeLen)
	}
	return normalized, nil
}

// loadDepGraph loads only the blocking (dep_type='blocks') edges. Non-blocking
// membership edges (parent-child, etc.) are excluded so cycle detection and the
// AddTypedDep reachability guard reason solely about ordering edges.
func loadDepGraph(db *gorm.DB) (map[string][]string, error) {
	var deps []gormIssueDep
	if err := db.Where("dep_type = ?", DepTypeBlocks).Find(&deps).Error; err != nil {
		return nil, fmt.Errorf("store: load dependency graph: %w", err)
	}
	graph := make(map[string][]string, len(deps))
	for _, dep := range deps {
		graph[dep.IssueID] = append(graph[dep.IssueID], dep.BlockedByID)
	}
	return graph, nil
}

func reaches(graph map[string][]string, start, target string) bool {
	stack := []string{start}
	seen := map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == target {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, graph[id]...)
	}
	return false
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func clockNowUTC() time.Time {
	return time.Now().UTC()
}

// idGenRetries is the number of attempts CreateIssue makes before giving up on
// ID generation. With 3 random bytes (16^6 ≈ 16.7M space per prefix), collisions
// are astronomically unlikely in practice; 5 retries is a safety net only.
const idGenRetries = 5

// generateID produces a "{prefix}-{shorthash}" id matching bd's format.
// Uses 3 random bytes → 6 lowercase hex chars.
func generateID(prefix string) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}

// storePriorityToCore maps the store's 0-indexed int (0=critical) to
// model.Priority's 1-indexed enum (PriorityCritical=1). Store 2 → PriorityMedium(3).
func storePriorityToCore(p int) model.Priority {
	// Store: 0=critical,1=high,2=medium,3=low,4=backlog
	// Core:  0=unset, 1=critical, 2=high, 3=medium, 4=low, 5=backlog
	cp := model.Priority(p + 1)
	if !cp.Valid() {
		return model.PriorityUnset
	}
	return cp
}

func issueType(s string) string {
	if s == "" {
		return "task"
	}
	return s
}

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// encodedLabels returns a JSON byte slice for the JSONB column.
func encodedLabels(labels []string) []byte {
	return []byte(jsonStringArray(labels))
}

// decodeLabels parses a JSONB []byte into a string slice.
// Returns nil on error or empty input.
func decodeLabels(b []byte) []string {
	return stringArrayFromJSON(b)
}

func dedupeImportInputs(items []ImportInput) []ImportInput {
	if len(items) < 2 {
		return items
	}
	out := make([]ImportInput, 0, len(items))
	indexByID := make(map[string]int, len(items))
	for _, item := range items {
		if idx, ok := indexByID[item.ID]; ok {
			// Contract: for a repeated id, edge slices (Deps, ParentEdges) are
			// UNIONED while scalar fields are REPLACED wholesale (last write wins).
			// The union is not de-duplicated here; downstream per-loop seen-maps
			// and the (issue_id, blocked_by_id) PK absorb any duplicate edges, so
			// a repeated edge is counted as skipped-duplicate, never written twice.
			deps := append([]string{}, out[idx].Deps...)
			deps = append(deps, item.Deps...)
			parents := append([]string{}, out[idx].ParentEdges...)
			parents = append(parents, item.ParentEdges...)
			out[idx] = item
			out[idx].Deps = deps
			out[idx].ParentEdges = parents
			continue
		}
		indexByID[item.ID] = len(out)
		out = append(out, item)
	}
	return out
}

func wrapExecErr(err error, op string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("store: %s: %w", op, normalizePoolError(err))
}
