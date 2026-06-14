package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mattsp1290/beans/model"
	repovalidation "github.com/mattsp1290/beans/repo"
	"github.com/mattsp1290/beans/schema"
)

// Issue is the richer store-layer representation. It embeds model.Issue (which
// callers and the tracker.Tracker adapter use) and adds IssueType — needed by
// the bn CLI for --json output.
type Issue struct {
	model.Issue
	IssueType string
}

// Store is the data access layer for the bn tracker. It owns the Postgres
// pool and all queries. Both the bn CLI (cmd/bn) and the Postgres tracker
// adapter call it directly — neither surface shells out to the other.
//
// Store is safe for concurrent use; the underlying pgxpool handles concurrency.
type Store struct {
	p *pool
}

// New dials the configured database, runs migrations, and returns a ready Store.
// Until the remaining pgx-backed methods are ported, non-Postgres stores are
// migration-ready but operational methods return ErrUnsupportedDriver.
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
	pool, err := s.p.conn()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO bn_projects (prefix) VALUES ($1) ON CONFLICT (prefix) DO NOTHING`,
		prefix,
	)
	return wrapExecErr(err, "EnsureProject")
}

// ProjectExists reports whether a project prefix is registered.
func (s *Store) ProjectExists(ctx context.Context, prefix string) (bool, error) {
	pool, err := s.p.conn()
	if err != nil {
		return false, err
	}
	var exists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bn_projects WHERE prefix = $1)`,
		prefix,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: ProjectExists: %w", err)
	}
	return exists, nil
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
type IssueRepoInput struct {
	RepoSlug       string
	RequestedRef   string
	BaseRef        string
	WorkBranch     string
	WorktreeSubdir string
	Metadata       map[string]any
}

// CreateIssue inserts a new issue and returns it with a generated ID.
// The project prefix must already be registered via EnsureProject.
// Retries on the rare hash collision (PK violation).
func (s *Store) CreateIssue(ctx context.Context, in CreateIssueInput) (Issue, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Issue{}, err
	}

	labels := encodedLabels(in.Labels)
	typ := issueType(in.IssueType)

	for range idGenRetries {
		id, genErr := generateID(in.Prefix)
		if genErr != nil {
			return Issue{}, fmt.Errorf("store: generate id: %w", genErr)
		}

		var (
			createdAt time.Time
			updatedAt time.Time
		)

		var repoTarget *model.RepoTarget
		if in.Repo != nil {
			tx, beginErr := pool.Begin(ctx)
			if beginErr != nil {
				return Issue{}, fmt.Errorf("store: CreateIssue begin tx: %w", beginErr)
			}
			err = tx.QueryRow(ctx, `
				INSERT INTO bn_issues
					(id, prefix, title, description, priority, issue_type, labels, branch_name, url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				RETURNING created_at, updated_at`,
				id, in.Prefix, in.Title, in.Description,
				in.Priority, typ, labels,
				nullableStr(in.BranchName), nullableStr(in.URL),
			).Scan(&createdAt, &updatedAt)
			if err == nil {
				repoTarget, err = insertIssueRepo(ctx, tx, id, in.Prefix, *in.Repo)
			}
			if err == nil && in.Actor != "" {
				_, err = tx.Exec(ctx,
					`INSERT INTO bn_issue_notes (issue_id, actor, body) VALUES ($1, $2, $3)`,
					id, in.Actor, fmt.Sprintf("created by %s", in.Actor),
				)
				if err != nil {
					err = fmt.Errorf("store: CreateIssue note: %w", err)
				}
			}
			if err == nil {
				err = tx.Commit(ctx)
			}
			if err != nil {
				_ = tx.Rollback(ctx)
			}
		} else {
			err = pool.QueryRow(ctx, `
				INSERT INTO bn_issues
					(id, prefix, title, description, priority, issue_type, labels, branch_name, url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				RETURNING created_at, updated_at`,
				id, in.Prefix, in.Title, in.Description,
				in.Priority, typ, labels,
				nullableStr(in.BranchName), nullableStr(in.URL),
			).Scan(&createdAt, &updatedAt)

			if err == nil && in.Actor != "" {
				_, noteErr := pool.Exec(ctx,
					`INSERT INTO bn_issue_notes (issue_id, actor, body) VALUES ($1, $2, $3)`,
					id, in.Actor, fmt.Sprintf("created by %s", in.Actor),
				)
				if noteErr != nil {
					return Issue{}, fmt.Errorf("store: CreateIssue note: %w", noteErr)
				}
			}
		}

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
					CreatedAt:   createdAt.UTC(),
					UpdatedAt:   updatedAt.UTC(),
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
// single batched query (WHERE id = ANY($1)), not one call per ID.
func (s *Store) GetIssueStatesByIDs(ctx context.Context, ids []string) (map[string]model.IssueState, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx,
		`SELECT id, state FROM bn_issues WHERE id = ANY($1)`,
		ids,
	)
	if err != nil {
		return nil, fmt.Errorf("store: GetIssueStatesByIDs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]model.IssueState, len(ids))
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, fmt.Errorf("store: GetIssueStatesByIDs scan: %w", err)
		}
		result[id] = model.IssueState(state)
	}
	return result, rows.Err()
}

// GetIssue returns a single issue with its BlockedBy list populated.
// Returns ErrNotFound if no issue with that id exists.
func (s *Store) GetIssue(ctx context.Context, id string) (Issue, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Issue{}, err
	}

	row := pool.QueryRow(ctx, `
		SELECT id, identifier, title, description, priority, issue_type, state,
		       labels, branch_name, url, created_at, updated_at
		FROM bn_issues WHERE id = $1`, id)

	iss, err := scanIssue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, fmt.Errorf("store: %w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Issue{}, fmt.Errorf("store: GetIssue: %w", err)
	}

	iss.BlockedBy, err = s.fetchBlockedBy(ctx, pool, id)
	if err != nil {
		return Issue{}, err
	}
	issues := []Issue{iss}
	if err := s.populateIssueRepos(ctx, pool, issues); err != nil {
		return Issue{}, err
	}
	iss = issues[0]
	return iss, nil
}

// ListFilter scopes a ListIssues call.
type ListFilter struct {
	Prefix string
	States []model.IssueState // nil/empty = all states
	Limit  int                // 0 = no limit
}

// ListIssues returns issues matching the filter with BlockedBy populated.
func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]Issue, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}

	args := []any{f.Prefix}
	q := `SELECT id, identifier, title, description, priority, issue_type, state,
		         labels, branch_name, url, created_at, updated_at
		  FROM bn_issues WHERE prefix = $1`

	if len(f.States) > 0 {
		strs := make([]string, len(f.States))
		for i, st := range f.States {
			args = append(args, string(st))
			strs[i] = fmt.Sprintf("$%d", len(args))
		}
		q += " AND state IN (" + strings.Join(strs, ",") + ")"
	}

	q += " ORDER BY priority ASC, created_at ASC"

	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListIssues: %w", err)
	}
	defer rows.Close()

	issues, err := collectIssues(rows)
	if err != nil {
		return nil, err
	}

	if err := s.populateBlockedBy(ctx, pool, issues); err != nil {
		return nil, err
	}
	if err := s.populateIssueRepos(ctx, pool, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// ReadyIssues returns issues that are open (state in activeStates) and have
// all blockers in a terminal state. The terminal set is caller-supplied so
// the ready semantics match the operator's WorkspaceConfig.TerminalStates
// (never hardcoded to "closed").
//
// Cross-prefix deps: an issue is only considered blocked by issues in the
// same Postgres instance. Dangling edges (blocked_by_id references a deleted
// issue) are handled by ON DELETE CASCADE — if the blocker is deleted, the
// edge disappears and the child becomes unblocked automatically.
func (s *Store) ReadyIssues(ctx context.Context, prefix string, terminalStates []model.IssueState, activeStates []model.IssueState) ([]Issue, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}

	if len(activeStates) == 0 {
		return []Issue{}, nil
	}

	args := []any{prefix}

	activePlaceholders := make([]string, len(activeStates))
	for i, st := range activeStates {
		args = append(args, string(st))
		activePlaceholders[i] = fmt.Sprintf("$%d", len(args))
	}

	var q string
	if len(terminalStates) == 0 {
		// When no states are terminal, every blocker is unsatisfied — so only
		// issues with no blockers at all are ready. Use the simpler EXISTS
		// form to avoid the invalid "NOT IN ()" SQL syntax.
		q = fmt.Sprintf(`
			SELECT i.id, i.identifier, i.title, i.description, i.priority, i.issue_type,
			       i.state, i.labels, i.branch_name, i.url, i.created_at, i.updated_at
			FROM bn_issues i
			WHERE i.prefix = $1
			  AND i.state IN (%s)
			  AND NOT EXISTS (
			    SELECT 1 FROM bn_issue_deps d WHERE d.issue_id = i.id
			  )
			ORDER BY i.priority ASC, i.created_at ASC`,
			strings.Join(activePlaceholders, ","),
		)
	} else {
		termPlaceholders := make([]string, len(terminalStates))
		for i, st := range terminalStates {
			args = append(args, string(st))
			termPlaceholders[i] = fmt.Sprintf("$%d", len(args))
		}
		q = fmt.Sprintf(`
			SELECT i.id, i.identifier, i.title, i.description, i.priority, i.issue_type,
			       i.state, i.labels, i.branch_name, i.url, i.created_at, i.updated_at
			FROM bn_issues i
			WHERE i.prefix = $1
			  AND i.state IN (%s)
			  AND NOT EXISTS (
			    SELECT 1 FROM bn_issue_deps d
			    JOIN bn_issues b ON b.id = d.blocked_by_id
			    WHERE d.issue_id = i.id
			      AND b.state NOT IN (%s)
			  )
			ORDER BY i.priority ASC, i.created_at ASC`,
			strings.Join(activePlaceholders, ","),
			strings.Join(termPlaceholders, ","),
		)
	}

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ReadyIssues: %w", err)
	}
	defer rows.Close()

	issues, err := collectIssues(rows)
	if err != nil {
		return nil, err
	}

	if err := s.populateBlockedBy(ctx, pool, issues); err != nil {
		return nil, err
	}
	if err := s.populateIssueRepos(ctx, pool, issues); err != nil {
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
	pool, err := s.p.conn()
	if err != nil {
		return Issue{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Issue{}, fmt.Errorf("store: UpdateIssue begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	setClauses := []string{}
	args := []any{}

	argN := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if in.Title != nil {
		setClauses = append(setClauses, "title = "+argN(*in.Title))
	}
	if in.Description != nil {
		setClauses = append(setClauses, "description = "+argN(*in.Description))
	}
	if in.Priority != nil {
		setClauses = append(setClauses, "priority = "+argN(*in.Priority))
	}
	if in.State != nil {
		setClauses = append(setClauses, "state = "+argN(string(*in.State)))
	}
	if in.Labels != nil {
		setClauses = append(setClauses, "labels = "+argN(encodedLabels(in.Labels)))
	}
	if in.BranchName != nil {
		setClauses = append(setClauses, "branch_name = "+argN(nullableStr(*in.BranchName)))
	}
	if in.URL != nil {
		setClauses = append(setClauses, "url = "+argN(nullableStr(*in.URL)))
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = now()")
		args = append(args, id)
		q := fmt.Sprintf(
			"UPDATE bn_issues SET %s WHERE id = $%d",
			strings.Join(setClauses, ", "),
			len(args),
		)
		tag, err := tx.Exec(ctx, q, args...)
		if err != nil {
			return Issue{}, fmt.Errorf("store: UpdateIssue: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return Issue{}, fmt.Errorf("store: %w: %s", ErrNotFound, id)
		}
	}

	var prefix string
	if in.Repo != nil || len(setClauses) == 0 {
		err := tx.QueryRow(ctx, `SELECT prefix FROM bn_issues WHERE id = $1`, id).Scan(&prefix)
		if errors.Is(err, pgx.ErrNoRows) {
			return Issue{}, fmt.Errorf("store: %w: %s", ErrNotFound, id)
		}
		if err != nil {
			return Issue{}, fmt.Errorf("store: UpdateIssue fetch prefix: %w", err)
		}
	}

	if in.Repo != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM bn_issue_repos WHERE issue_id = $1`, id); err != nil {
			return Issue{}, fmt.Errorf("store: UpdateIssue repo delete: %w", err)
		}
		if _, err := insertIssueRepo(ctx, tx, id, prefix, *in.Repo); err != nil {
			return Issue{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE bn_issues SET updated_at = now() WHERE id = $1`, id); err != nil {
			return Issue{}, fmt.Errorf("store: UpdateIssue repo timestamp: %w", err)
		}
	}
	if in.AppendNotes != nil && strings.TrimSpace(in.AppendNotes.Body) != "" {
		_, err := tx.Exec(ctx,
			`INSERT INTO bn_issue_notes (issue_id, actor, body) VALUES ($1, $2, $3)`,
			id, nullableStr(in.AppendNotes.Actor), in.AppendNotes.Body,
		)
		if err != nil {
			return Issue{}, fmt.Errorf("store: UpdateIssue notes: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Issue{}, fmt.Errorf("store: UpdateIssue commit: %w", err)
	}
	committed = true
	return s.GetIssue(ctx, id)
}

// CloseIssue sets an issue's state to "closed". Idempotent: closing an already-
// closed issue returns nil without inserting a duplicate note or bumping updated_at.
// If reason is non-empty it is appended as a note only when the state actually changes.
func (s *Store) CloseIssue(ctx context.Context, id, actor, reason string) error {
	pool, err := s.p.conn()
	if err != nil {
		return err
	}

	// Only update when the issue is not already closed so that repeated calls don't
	// accumulate noise notes or churn updated_at.
	tag, err := pool.Exec(ctx,
		`UPDATE bn_issues SET state = 'closed', updated_at = now() WHERE id = $1 AND state != 'closed'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("store: CloseIssue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either the issue does not exist, or it was already closed.
		exists, checkErr := s.issueExists(ctx, pool, id)
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
		_, err = pool.Exec(ctx,
			`INSERT INTO bn_issue_notes (issue_id, actor, body) VALUES ($1, $2, $3)`,
			id, nullableStr(actor), strings.TrimSpace(reason),
		)
		if err != nil {
			return fmt.Errorf("store: CloseIssue note: %w", err)
		}
	}
	return nil
}

// DeleteIssue hard-deletes an issue and its dependent edges/notes via CASCADE.
// Returns ErrNotFound if the issue does not exist.
func (s *Store) DeleteIssue(ctx context.Context, id string) error {
	pool, err := s.p.conn()
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, `DELETE FROM bn_issues WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: DeleteIssue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: %w: %s", ErrNotFound, id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dependency management
// ---------------------------------------------------------------------------

// AddDep adds an edge: childID is blocked until parentID reaches a terminal
// state. Returns ErrCycle if the edge would create a cycle, ErrDuplicateDep
// if the edge already exists, or ErrNotFound if either issue is missing.
// All checks and the INSERT run inside a single SERIALIZABLE transaction so
// concurrent AddDep calls cannot form a cycle via a TOCTOU race.
func (s *Store) AddDep(ctx context.Context, childID, parentID string) error {
	pgxPool, err := s.p.conn()
	if err != nil {
		return err
	}

	tx, err := pgxPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("store: AddDep begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Verify both issues exist.
	for _, id := range []string{childID, parentID} {
		exists, err := s.issueExists(ctx, tx, id)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("store: %w: %s", ErrNotFound, id)
		}
	}

	// Cycle check: parentID must not be reachable from childID via existing edges.
	if cycle, err := s.hasCycle(ctx, tx, childID, parentID); err != nil {
		return err
	} else if cycle {
		return fmt.Errorf("store: %w: %s → %s", ErrCycle, childID, parentID)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO bn_issue_deps (issue_id, blocked_by_id) VALUES ($1, $2)`,
		childID, parentID,
	)
	if err != nil {
		if isDupKeyConflict(err) {
			return ErrDuplicateDep
		}
		return fmt.Errorf("store: AddDep: %w", err)
	}
	return tx.Commit(ctx)
}

// RemoveDep removes a dependency edge. Returns ErrNotFound if the edge does
// not exist.
func (s *Store) RemoveDep(ctx context.Context, childID, parentID string) error {
	pool, err := s.p.conn()
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx,
		`DELETE FROM bn_issue_deps WHERE issue_id = $1 AND blocked_by_id = $2`,
		childID, parentID,
	)
	if err != nil {
		return fmt.Errorf("store: RemoveDep: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: %w: dep %s → %s", ErrNotFound, childID, parentID)
	}
	return nil
}

// DepEdge is one directed dependency edge returned by ListDeps.
type DepEdge struct {
	IssueID     string
	BlockedByID string
}

// ListDeps returns all dependency edges for issues in the given prefix.
func (s *Store) ListDeps(ctx context.Context, prefix string) ([]DepEdge, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		SELECT d.issue_id, d.blocked_by_id
		FROM bn_issue_deps d
		JOIN bn_issues i ON i.id = d.issue_id
		WHERE i.prefix = $1
		ORDER BY d.issue_id, d.blocked_by_id`,
		prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("store: ListDeps: %w", err)
	}
	defer rows.Close()

	var edges []DepEdge
	for rows.Next() {
		var e DepEdge
		if err := rows.Scan(&e.IssueID, &e.BlockedByID); err != nil {
			return nil, fmt.Errorf("store: ListDeps scan: %w", err)
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
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
	Deps        []string // blocked_by ids
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
	pgxPool, err := s.p.conn()
	if err != nil {
		return err
	}

	terminalSet := make(map[string]bool, len(terminalStates))
	for _, st := range terminalStates {
		terminalSet[string(st)] = true
	}

	tx, err := pgxPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: ImportIssues begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Pass 1: upsert issues.
	for _, item := range items {
		labels := encodedLabels(item.Labels)
		if terminalSet[item.State] {
			// Insert if absent; if present and terminal, update non-state fields only.
			_, err = tx.Exec(ctx, `
				INSERT INTO bn_issues
					(id, prefix, title, description, priority, issue_type, state, labels, branch_name, url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (id) DO UPDATE SET
					title       = EXCLUDED.title,
					description = EXCLUDED.description,
					priority    = EXCLUDED.priority,
					issue_type  = EXCLUDED.issue_type,
					labels      = EXCLUDED.labels,
					branch_name = EXCLUDED.branch_name,
					url         = EXCLUDED.url,
					updated_at  = now()`,
				item.ID, item.Prefix, item.Title, item.Description,
				item.Priority, issueType(item.IssueType), item.State, labels,
				nullableStr(item.BranchName), nullableStr(item.URL),
			)
		} else {
			// Insert if absent; if present, update all non-state-terminal fields.
			// State is only updated if the existing state is NOT terminal.
			_, err = tx.Exec(ctx, `
				INSERT INTO bn_issues
					(id, prefix, title, description, priority, issue_type, state, labels, branch_name, url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (id) DO UPDATE SET
					title       = EXCLUDED.title,
					description = EXCLUDED.description,
					priority    = EXCLUDED.priority,
					issue_type  = EXCLUDED.issue_type,
					state       = CASE WHEN bn_issues.state = ANY($11::text[])
					                   THEN bn_issues.state
					                   ELSE EXCLUDED.state END,
					labels      = EXCLUDED.labels,
					branch_name = EXCLUDED.branch_name,
					url         = EXCLUDED.url,
					updated_at  = now()`,
				item.ID, item.Prefix, item.Title, item.Description,
				item.Priority, issueType(item.IssueType), item.State, labels,
				nullableStr(item.BranchName), nullableStr(item.URL),
				terminalStateArray(terminalStates),
			)
		}
		if err != nil {
			return fmt.Errorf("store: ImportIssues upsert %s: %w", item.ID, err)
		}
	}

	// Pass 2: insert dep edges (forward refs resolved because all issues are now present).
	for _, item := range items {
		for _, blockerID := range item.Deps {
			_, err = tx.Exec(ctx, `
				INSERT INTO bn_issue_deps (issue_id, blocked_by_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING`,
				item.ID, blockerID,
			)
			if err != nil {
				return fmt.Errorf("store: ImportIssues dep %s→%s: %w", item.ID, blockerID, err)
			}
		}
	}

	return tx.Commit(ctx)
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
	TerminalStates []model.IssueState
	Mode           ImportMode
}

// ImportResult counts what ImportIssuesFull did.
type ImportResult struct {
	Created                   int // issues inserted for the first time
	Updated                   int // issues that already existed (merge mode only)
	Skipped                   int // issues skipped because they already existed (create-only)
	CrossPrefixConflicts      int // issue IDs already owned by another prefix
	DepsAdded                 int // dep edges successfully inserted
	DepsSkippedMissingBlocker int // dep edges skipped — blocker not in destination prefix
	DepsSkippedDuplicate      int // dep edges skipped because they already existed
	DepsSkippedSelf           int // dep edges skipped because issue_id == blocked_by_id
	DepsSkippedCycle          int // dep edges skipped because they would create a cycle
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
	pool, err := s.p.conn()
	if err != nil {
		return ImportResult{}, err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ImportResult{}, fmt.Errorf("store: ImportIssuesFull begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Track which items were actually written so pass-2 can skip the rest.
	written := make(map[string]bool, len(items))

	var result ImportResult

	// Pass 1: upsert issues.
	for _, item := range items {
		labels := encodedLabels(item.Labels)
		existing, alreadyExists, err := s.getImportIssueSnapshot(ctx, tx, item.ID)
		if err != nil {
			return ImportResult{}, err
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
			tag, execErr := tx.Exec(ctx, `
				INSERT INTO bn_issues
					(id, prefix, title, description, priority, issue_type, state, labels, branch_name, url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (id) DO NOTHING`,
				item.ID, item.Prefix, item.Title, item.Description,
				item.Priority, issueType(item.IssueType), item.State, labels,
				nullableStr(item.BranchName), nullableStr(item.URL),
			)
			if execErr != nil {
				return ImportResult{}, fmt.Errorf("store: ImportIssuesFull create %s: %w", item.ID, execErr)
			}
			if tag.RowsAffected() > 0 {
				result.Created++
				written[item.ID] = true
			} else {
				result.Skipped++
			}
		} else {
			tag, execErr := tx.Exec(ctx, `
				INSERT INTO bn_issues
					(id, prefix, title, description, priority, issue_type, state, labels, branch_name, url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (id) DO UPDATE SET
					title       = EXCLUDED.title,
					description = EXCLUDED.description,
					priority    = EXCLUDED.priority,
					issue_type  = EXCLUDED.issue_type,
					state       = CASE WHEN bn_issues.state = ANY($11::text[])
					                   THEN bn_issues.state
					                   ELSE EXCLUDED.state END,
					labels      = EXCLUDED.labels,
					branch_name = EXCLUDED.branch_name,
					url         = EXCLUDED.url,
					updated_at  = now()`,
				item.ID, item.Prefix, item.Title, item.Description,
				item.Priority, issueType(item.IssueType), item.State, labels,
				nullableStr(item.BranchName), nullableStr(item.URL),
				terminalStateArray(opts.TerminalStates),
			)
			if execErr != nil {
				return ImportResult{}, fmt.Errorf("store: ImportIssuesFull merge %s: %w", item.ID, execErr)
			}
			if tag.RowsAffected() > 0 {
				if alreadyExists {
					result.Updated++
				} else {
					result.Created++
				}
				written[item.ID] = true
			}
		}
	}

	// Pass 2: insert dep edges for written items only.
	// Only insert edges whose blocker exists in the destination prefix to avoid
	// FK violations and cross-prefix graph leakage.
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
				return ImportResult{}, err
			} else if !exists {
				result.DepsSkippedMissingBlocker++
				continue
			}
			if cycle, err := s.hasCycle(ctx, tx, item.ID, blockerID); err != nil {
				return ImportResult{}, err
			} else if cycle {
				result.DepsSkippedCycle++
				continue
			}
			tag, execErr := tx.Exec(ctx, `
				INSERT INTO bn_issue_deps (issue_id, blocked_by_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING`,
				item.ID, blockerID,
			)
			if execErr != nil {
				return ImportResult{}, fmt.Errorf("store: ImportIssuesFull dep %s→%s: %w", item.ID, blockerID, execErr)
			}
			if tag.RowsAffected() > 0 {
				result.DepsAdded++
			} else {
				result.DepsSkippedDuplicate++
			}
		}
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return ImportResult{}, fmt.Errorf("store: ImportIssuesFull commit: %w", commitErr)
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

// InsertMemory adds a new memory row. An empty Prefix stores NULL (global).
// For non-global memories, the prefix must be registered in bn_projects.
func (s *Store) InsertMemory(ctx context.Context, in MemoryInput) (Memory, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Memory{}, err
	}

	tags := encodedLabels(in.Tags) // reuse the JSON-array encoder
	var prefix *string
	if in.Prefix != "" {
		prefix = &in.Prefix
	}
	var mtype *string
	if in.Type != "" {
		mtype = &in.Type
	}

	var (
		id        int64
		createdAt time.Time
	)
	err = pool.QueryRow(ctx,
		`INSERT INTO bn_memories (prefix, body, mtype, tags) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		prefix, in.Body, mtype, tags,
	).Scan(&id, &createdAt)
	if err != nil {
		return Memory{}, fmt.Errorf("store: InsertMemory: %w", err)
	}

	return Memory{
		ID:        id,
		Prefix:    prefix,
		Body:      in.Body,
		Type:      mtype,
		Tags:      in.Tags,
		CreatedAt: createdAt.UTC(),
	}, nil
}

// SearchMemories returns memories matching the filter. When query is non-empty,
// full-text search is used (plainto_tsquery for safe user input). When empty,
// recent memories are returned ordered by created_at DESC.
// Scope: when All=false, returns rows for Prefix + global (prefix IS NULL).
func (s *Store) SearchMemories(ctx context.Context, query string, f MemoryFilter) ([]Memory, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)

	limit := f.Limit
	if limit <= 0 {
		limit = defaultMemoryLimit
	}

	args := []any{}
	argN := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	q := `SELECT id, prefix, body, mtype, tags, created_at FROM bn_memories WHERE TRUE`

	if !f.All {
		// Scope to active project + globals.
		pfx := f.Prefix
		q += fmt.Sprintf(" AND (prefix = %s OR prefix IS NULL)", argN(pfx))
	}

	// Capture the query placeholder once so both the WHERE and ORDER BY clauses
	// reference the same positional parameter — type/tag filters added after this
	// would otherwise shift len(args) and cause ts_rank to rank by the wrong value.
	var queryPlaceholder string
	if query != "" {
		queryPlaceholder = argN(query)
		q += " AND tsv @@ plainto_tsquery('english', " + queryPlaceholder + ")"
	}

	if f.Type != "" {
		q += fmt.Sprintf(" AND mtype = %s", argN(f.Type))
	}

	if len(f.Tags) > 0 {
		tagJSON := encodedLabels(f.Tags)
		q += fmt.Sprintf(" AND tags @> %s::jsonb", argN(string(tagJSON)))
	}

	if query != "" {
		q += " ORDER BY ts_rank(tsv, plainto_tsquery('english', " + queryPlaceholder + ")) DESC, created_at DESC, id DESC"
	} else {
		q += " ORDER BY created_at DESC, id DESC"
	}

	q += fmt.Sprintf(" LIMIT %s", argN(limit))

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: SearchMemories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var tagsBytes []byte
		err := rows.Scan(&m.ID, &m.Prefix, &m.Body, &m.Type, &tagsBytes, &m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: SearchMemories scan: %w", err)
		}
		m.Tags = decodeLabels(tagsBytes)
		m.CreatedAt = m.CreatedAt.UTC()
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Store) issueExists(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bn_issues WHERE id = $1)`, id,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: issueExists: %w", err)
	}
	return exists, nil
}

func (s *Store) issueExistsInPrefix(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id, prefix string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bn_issues WHERE id = $1 AND prefix = $2)`, id, prefix,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: issueExistsInPrefix: %w", err)
	}
	return exists, nil
}

type importIssueSnapshot struct {
	Prefix string
	State  model.IssueState
}

func (s *Store) getImportIssueSnapshot(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (importIssueSnapshot, bool, error) {
	var snap importIssueSnapshot
	var state string
	err := pool.QueryRow(ctx,
		`SELECT prefix, state FROM bn_issues WHERE id = $1`, id,
	).Scan(&snap.Prefix, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return importIssueSnapshot{}, false, nil
	}
	if err != nil {
		return importIssueSnapshot{}, false, fmt.Errorf("store: getImportIssueSnapshot: %w", err)
	}
	snap.State = model.IssueState(state)
	return snap, true, nil
}

// hasCycle returns true if adding childID→parentID would create a cycle,
// using a recursive CTE to walk the existing dependency graph.
func (s *Store) hasCycle(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, childID, parentID string) (bool, error) {
	// If childID is reachable from parentID (i.e., parentID depends on
	// childID directly or transitively), adding childID→parentID creates a cycle.
	var reachable bool
	err := pool.QueryRow(ctx, `
		WITH RECURSIVE ancestors(id) AS (
			SELECT $1::text
			UNION ALL
			SELECT d.blocked_by_id
			FROM bn_issue_deps d
			JOIN ancestors a ON a.id = d.issue_id
		)
		SELECT EXISTS (SELECT 1 FROM ancestors WHERE id = $2)`,
		parentID, childID,
	).Scan(&reachable)
	if err != nil {
		return false, fmt.Errorf("store: cycle check: %w", err)
	}
	return reachable, nil
}

func (s *Store) fetchBlockedBy(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, id string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT blocked_by_id FROM bn_issue_deps WHERE issue_id = $1 ORDER BY blocked_by_id`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("store: fetchBlockedBy: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var bid string
		if err := rows.Scan(&bid); err != nil {
			return nil, fmt.Errorf("store: fetchBlockedBy scan: %w", err)
		}
		ids = append(ids, bid)
	}
	return ids, rows.Err()
}

func (s *Store) populateBlockedBy(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, issues []Issue) error {
	if len(issues) == 0 {
		return nil
	}

	ids := make([]string, len(issues))
	idxByID := make(map[string]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		idxByID[iss.ID] = i
	}

	rows, err := pool.Query(ctx, `
		SELECT issue_id, blocked_by_id
		FROM bn_issue_deps
		WHERE issue_id = ANY($1)
		ORDER BY issue_id, blocked_by_id`,
		ids,
	)
	if err != nil {
		return fmt.Errorf("store: populateBlockedBy: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var issueID, blockerID string
		if err := rows.Scan(&issueID, &blockerID); err != nil {
			return fmt.Errorf("store: populateBlockedBy scan: %w", err)
		}
		if idx, ok := idxByID[issueID]; ok {
			issues[idx].BlockedBy = append(issues[idx].BlockedBy, blockerID)
		}
	}
	return rows.Err()
}

func insertIssueRepo(ctx context.Context, tx pgx.Tx, issueID, prefix string, in IssueRepoInput) (*model.RepoTarget, error) {
	repo, err := getRepoBySlug(ctx, tx, prefix, in.RepoSlug)
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

	_, err = tx.Exec(ctx, `
		INSERT INTO bn_issue_repos
			(issue_id, repo_id, requested_ref, base_ref, work_branch, worktree_subdir, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		issueID, repo.ID, requestedRef, baseRef, workBranch, worktreeSubdir, metadata,
	)
	if err != nil {
		return nil, fmt.Errorf("store: CreateIssue repo: %w", err)
	}

	return repoTargetFromIssueRepo(repo, requestedRef, baseRef, workBranch, worktreeSubdir, in.Metadata), nil
}

func (s *Store) populateIssueRepos(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, issues []Issue) error {
	if len(issues) == 0 {
		return nil
	}

	ids := make([]string, len(issues))
	idxByID := make(map[string]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		idxByID[iss.ID] = i
	}

	rows, err := pool.Query(ctx, `
		SELECT ir.issue_id,
		       r.id, r.slug, r.remote_url, r.default_branch,
		       ir.requested_ref, ir.base_ref, ir.work_branch,
		       COALESCE(NULLIF(ir.worktree_subdir, ''), r.worktree_subdir),
		       r.clone_strategy, r.auth_ref, ir.metadata
		FROM bn_issue_repos ir
		JOIN bn_repos r ON r.id = ir.repo_id
		WHERE ir.issue_id = ANY($1)`,
		ids,
	)
	if err != nil {
		return fmt.Errorf("store: populateIssueRepos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			issueID  string
			target   model.RepoTarget
			metadata []byte
		)
		if err := rows.Scan(
			&issueID, &target.ID, &target.Slug, &target.RemoteURL, &target.DefaultBranch,
			&target.RequestedRef, &target.BaseRef, &target.WorkBranch, &target.WorktreeSubdir,
			&target.CloneStrategy, &target.AuthRef, &metadata,
		); err != nil {
			return fmt.Errorf("store: populateIssueRepos scan: %w", err)
		}
		target.Metadata = decodeJSONObject(metadata)
		if idx, ok := idxByID[issueID]; ok {
			issues[idx].Repo = &target
		}
	}
	return rows.Err()
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

// scanIssue scans one issue row (without BlockedBy) from a pgx.Row.
func scanIssue(row pgx.Row) (Issue, error) {
	var (
		id, title, description, issType, state string
		identifier                             *string
		priority                               int
		labels                                 []byte
		branchName, url                        *string
		createdAt, updatedAt                   time.Time
	)
	err := row.Scan(
		&id, &identifier, &title, &description,
		&priority, &issType, &state,
		&labels, &branchName, &url,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return Issue{}, err
	}

	return Issue{
		Issue: model.Issue{
			ID:          id,
			Identifier:  derefStr(identifier, id),
			Title:       title,
			Description: description,
			Priority:    storePriorityToCore(priority),
			State:       model.IssueState(state),
			Labels:      decodeLabels(labels),
			BranchName:  derefStr(branchName, ""),
			URL:         derefStr(url, ""),
			CreatedAt:   createdAt.UTC(),
			UpdatedAt:   updatedAt.UTC(),
		},
		IssueType: issType,
	}, nil
}

// collectIssues drains a rows result set into a slice (without BlockedBy).
func collectIssues(rows pgx.Rows) ([]Issue, error) {
	var issues []Issue
	for rows.Next() {
		var (
			id, title, description, issType, state string
			identifier                             *string
			priority                               int
			labels                                 []byte
			branchName, url                        *string
			createdAt, updatedAt                   time.Time
		)
		err := rows.Scan(
			&id, &identifier, &title, &description,
			&priority, &issType, &state,
			&labels, &branchName, &url,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: collectIssues scan: %w", err)
		}
		issues = append(issues, Issue{
			Issue: model.Issue{
				ID:          id,
				Identifier:  derefStr(identifier, id),
				Title:       title,
				Description: description,
				Priority:    storePriorityToCore(priority),
				State:       model.IssueState(state),
				Labels:      decodeLabels(labels),
				BranchName:  derefStr(branchName, ""),
				URL:         derefStr(url, ""),
				CreatedAt:   createdAt.UTC(),
				UpdatedAt:   updatedAt.UTC(),
			},
			IssueType: issType,
		})
	}
	return issues, rows.Err()
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

// terminalStateArray converts []model.IssueState to []string for the $N::text[] cast.
func terminalStateArray(states []model.IssueState) []string {
	out := make([]string, len(states))
	for i, s := range states {
		out[i] = string(s)
	}
	return out
}

func dedupeImportInputs(items []ImportInput) []ImportInput {
	if len(items) < 2 {
		return items
	}
	out := make([]ImportInput, 0, len(items))
	indexByID := make(map[string]int, len(items))
	for _, item := range items {
		if idx, ok := indexByID[item.ID]; ok {
			deps := append([]string{}, out[idx].Deps...)
			deps = append(deps, item.Deps...)
			out[idx] = item
			out[idx].Deps = deps
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
