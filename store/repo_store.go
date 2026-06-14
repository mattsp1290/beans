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

	"github.com/jackc/pgx/v5"

	"github.com/mattsp1290/beans/repo"
)

// Repo is one onboarded repository in the bn registry.
type Repo struct {
	ID             string
	Prefix         string
	Slug           string
	DisplayName    string
	RemoteURL      string
	DefaultBranch  string
	WorktreeSubdir string
	CloneStrategy  string
	AuthRef        string
	Enabled        bool
	Metadata       map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      string
	UpdatedBy      string
}

// CreateRepoInput carries the fields needed to onboard a repo.
type CreateRepoInput struct {
	Prefix         string
	Slug           string
	DisplayName    string
	RemoteURL      string
	DefaultBranch  string
	WorktreeSubdir string
	CloneStrategy  string
	AuthRef        string
	Metadata       map[string]any
	Actor          string
	Aliases        []string
}

// UpdateRepoInput carries optional repo mutations. nil pointer means keep.
type UpdateRepoInput struct {
	DisplayName    *string
	RemoteURL      *string
	DefaultBranch  *string
	WorktreeSubdir *string
	CloneStrategy  *string
	AuthRef        *string
	Enabled        *bool
	Metadata       map[string]any // nil = keep current; non-nil replaces
	Actor          string
	Aliases        []string // nil = keep current; non-nil replaces all aliases for repo
}

// RepoAuditInput carries one repo-audit event.
type RepoAuditInput struct {
	Prefix    string
	RepoID    string
	Action    string
	Actor     string
	OldValues map[string]any
	NewValues map[string]any
	Command   string
}

// RepoAudit is one redacted audit row for repo registry mutations.
type RepoAudit struct {
	ID        int64
	Prefix    string
	RepoID    *string
	Action    string
	Actor     string
	OldValues map[string]any
	NewValues map[string]any
	Command   string
	CreatedAt time.Time
}

// AddRepoAdmin adds targetActor to the project admin set. When bootstrap is
// true, the insert is allowed only if the project currently has no admins.
// Otherwise actor must already be an admin.
func (s *Store) AddRepoAdmin(ctx context.Context, prefix, targetActor, actor string, bootstrap bool) error {
	pool, err := s.p.conn()
	if err != nil {
		return err
	}
	if strings.TrimSpace(targetActor) == "" {
		return fmt.Errorf("store: AddRepoAdmin: actor must not be empty")
	}

	if bootstrap {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("store: AddRepoAdmin bootstrap begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, prefix); err != nil {
			return fmt.Errorf("store: AddRepoAdmin bootstrap lock: %w", err)
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO bn_project_admins (prefix, actor)
			SELECT $1, $2
			WHERE NOT EXISTS (SELECT 1 FROM bn_project_admins WHERE prefix = $1)
			ON CONFLICT DO NOTHING`,
			prefix, targetActor,
		)
		if err != nil {
			return fmt.Errorf("store: AddRepoAdmin bootstrap: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrUnauthorized
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: AddRepoAdmin bootstrap commit: %w", err)
		}
		return nil
	}
	if err := s.AuthorizeRepoAdmin(ctx, prefix, actor); err != nil {
		return err
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO bn_project_admins (prefix, actor) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		prefix, targetActor,
	)
	if err != nil {
		return fmt.Errorf("store: AddRepoAdmin: %w", err)
	}
	return nil
}

// ListRepoAdmins returns project admins ordered by actor.
func (s *Store) ListRepoAdmins(ctx context.Context, prefix string) ([]string, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT actor FROM bn_project_admins WHERE prefix = $1 ORDER BY actor`,
		prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("store: ListRepoAdmins: %w", err)
	}
	defer rows.Close()

	var admins []string
	for rows.Next() {
		var actor string
		if err := rows.Scan(&actor); err != nil {
			return nil, fmt.Errorf("store: ListRepoAdmins scan: %w", err)
		}
		admins = append(admins, actor)
	}
	return admins, rows.Err()
}

// RemoveRepoAdmin removes targetActor from the project admin set. actor must
// already be an admin; this prevents arbitrary BN_DSN holders from stripping
// repo registry ownership.
func (s *Store) RemoveRepoAdmin(ctx context.Context, prefix, targetActor, actor string) error {
	pool, err := s.p.conn()
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: RemoveRepoAdmin begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, prefix); err != nil {
		return fmt.Errorf("store: RemoveRepoAdmin lock: %w", err)
	}
	var authorized bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bn_project_admins WHERE prefix = $1 AND actor = $2)`,
		prefix, actor,
	).Scan(&authorized); err != nil {
		return fmt.Errorf("store: RemoveRepoAdmin authorize: %w", err)
	}
	if !authorized {
		return ErrUnauthorized
	}

	var adminCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM bn_project_admins WHERE prefix = $1`,
		prefix,
	).Scan(&adminCount); err != nil {
		return fmt.Errorf("store: RemoveRepoAdmin count admins: %w", err)
	}
	if adminCount <= 1 && targetActor == actor {
		return fmt.Errorf("store: %w: cannot remove last repo admin", ErrConflict)
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM bn_project_admins WHERE prefix = $1 AND actor = $2`,
		prefix, targetActor,
	)
	if err != nil {
		return fmt.Errorf("store: RemoveRepoAdmin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: %w: repo admin %s", ErrNotFound, targetActor)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: RemoveRepoAdmin commit: %w", err)
	}
	return nil
}

// AuthorizeRepoAdmin returns nil when actor can mutate repo registry state.
func (s *Store) AuthorizeRepoAdmin(ctx context.Context, prefix, actor string) error {
	pool, err := s.p.conn()
	if err != nil {
		return err
	}
	var ok bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bn_project_admins WHERE prefix = $1 AND actor = $2)`,
		prefix, actor,
	).Scan(&ok)
	if err != nil {
		return fmt.Errorf("store: AuthorizeRepoAdmin: %w", err)
	}
	if !ok {
		return ErrUnauthorized
	}
	return nil
}

// CreateRepo inserts a repo, replaces aliases, and writes an audit row
// atomically. Repo registry mutation requires in.Actor to be a repo admin.
func (s *Store) CreateRepo(ctx context.Context, in CreateRepoInput) (Repo, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Repo{}, err
	}
	if err := s.AuthorizeRepoAdmin(ctx, in.Prefix, in.Actor); err != nil {
		return Repo{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Repo{}, fmt.Errorf("store: CreateRepo begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	id, err := generateRepoID()
	if err != nil {
		return Repo{}, err
	}
	displayName := in.DisplayName
	if displayName == "" {
		displayName = in.Slug
	}
	remoteURL := strings.TrimSpace(in.RemoteURL)
	defaultBranch := repo.NormalizeDefaultBranch(in.DefaultBranch)
	worktreeSubdir := strings.TrimSpace(in.WorktreeSubdir)
	cloneStrategy := repo.NormalizeCloneStrategy(in.CloneStrategy)
	authRef := strings.TrimSpace(in.AuthRef)
	if err := repo.ValidateTarget(repo.Target{
		RemoteURL:      remoteURL,
		DefaultBranch:  defaultBranch,
		WorktreeSubdir: worktreeSubdir,
		CloneStrategy:  cloneStrategy,
		AuthRef:        authRef,
	}); err != nil {
		return Repo{}, fmt.Errorf("store: CreateRepo validation: %w", err)
	}
	metadata, err := encodeJSONObject(in.Metadata)
	if err != nil {
		return Repo{}, fmt.Errorf("store: CreateRepo metadata: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO bn_repos
			(id, prefix, slug, display_name, remote_url, default_branch,
			 worktree_subdir, clone_strategy, auth_ref, metadata, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
		RETURNING id, prefix, slug, display_name, remote_url, default_branch,
		          worktree_subdir, clone_strategy, auth_ref, enabled, metadata,
		          created_at, updated_at, created_by, updated_by`,
		id, in.Prefix, in.Slug, displayName, remoteURL, defaultBranch,
		worktreeSubdir, cloneStrategy, authRef, metadata, in.Actor,
	)
	repo, err := scanRepo(row)
	if err != nil {
		if isDupKeyConflict(err) {
			return Repo{}, fmt.Errorf("store: %w: repo %s", ErrConflict, in.Slug)
		}
		return Repo{}, fmt.Errorf("store: CreateRepo: %w", err)
	}
	if err := replaceRepoAliases(ctx, tx, repo.Prefix, repo.ID, in.Aliases); err != nil {
		return Repo{}, err
	}
	if err := insertRepoAudit(ctx, tx, RepoAuditInput{
		Prefix:    repo.Prefix,
		RepoID:    repo.ID,
		Action:    "repo.create",
		Actor:     in.Actor,
		NewValues: repoAuditValues(repo),
	}); err != nil {
		return Repo{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Repo{}, fmt.Errorf("store: CreateRepo commit: %w", err)
	}
	return repo, nil
}

// UpdateRepo applies partial repo updates, optionally replaces aliases, and
// writes an audit row atomically. Repo registry mutation requires admin access.
func (s *Store) UpdateRepo(ctx context.Context, prefix, slug string, in UpdateRepoInput) (Repo, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Repo{}, err
	}
	if err := s.AuthorizeRepoAdmin(ctx, prefix, in.Actor); err != nil {
		return Repo{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Repo{}, fmt.Errorf("store: UpdateRepo begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	oldRepo, err := getRepoBySlug(ctx, tx, prefix, slug)
	if err != nil {
		return Repo{}, err
	}
	target := repo.Target{
		RemoteURL:      oldRepo.RemoteURL,
		DefaultBranch:  oldRepo.DefaultBranch,
		WorktreeSubdir: oldRepo.WorktreeSubdir,
		CloneStrategy:  oldRepo.CloneStrategy,
		AuthRef:        oldRepo.AuthRef,
	}
	if in.RemoteURL != nil {
		target.RemoteURL = strings.TrimSpace(*in.RemoteURL)
	}
	if in.DefaultBranch != nil {
		target.DefaultBranch = repo.NormalizeDefaultBranch(*in.DefaultBranch)
	}
	if in.WorktreeSubdir != nil {
		target.WorktreeSubdir = strings.TrimSpace(*in.WorktreeSubdir)
	}
	if in.CloneStrategy != nil {
		target.CloneStrategy = repo.NormalizeCloneStrategy(*in.CloneStrategy)
	}
	if in.AuthRef != nil {
		target.AuthRef = strings.TrimSpace(*in.AuthRef)
	}
	if err := repo.ValidateTarget(target); err != nil {
		return Repo{}, fmt.Errorf("store: UpdateRepo validation: %w", err)
	}

	setClauses := []string{"updated_at = now()", "updated_by = $1"}
	args := []any{in.Actor}
	argN := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if in.DisplayName != nil {
		setClauses = append(setClauses, "display_name = "+argN(*in.DisplayName))
	}
	if in.RemoteURL != nil {
		setClauses = append(setClauses, "remote_url = "+argN(target.RemoteURL))
	}
	if in.DefaultBranch != nil {
		setClauses = append(setClauses, "default_branch = "+argN(target.DefaultBranch))
	}
	if in.WorktreeSubdir != nil {
		setClauses = append(setClauses, "worktree_subdir = "+argN(target.WorktreeSubdir))
	}
	if in.CloneStrategy != nil {
		setClauses = append(setClauses, "clone_strategy = "+argN(target.CloneStrategy))
	}
	if in.AuthRef != nil {
		setClauses = append(setClauses, "auth_ref = "+argN(target.AuthRef))
	}
	if in.Enabled != nil {
		setClauses = append(setClauses, "enabled = "+argN(*in.Enabled))
	}
	if in.Metadata != nil {
		metadata, err := encodeJSONObject(in.Metadata)
		if err != nil {
			return Repo{}, fmt.Errorf("store: UpdateRepo metadata: %w", err)
		}
		setClauses = append(setClauses, "metadata = "+argN(metadata))
	}

	args = append(args, oldRepo.ID)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE bn_repos SET %s
		WHERE id = $%d
		RETURNING id, prefix, slug, display_name, remote_url, default_branch,
		          worktree_subdir, clone_strategy, auth_ref, enabled, metadata,
		          created_at, updated_at, created_by, updated_by`,
		strings.Join(setClauses, ", "), len(args),
	), args...)
	repo, err := scanRepo(row)
	if err != nil {
		return Repo{}, fmt.Errorf("store: UpdateRepo: %w", err)
	}
	if in.Aliases != nil {
		if err := replaceRepoAliases(ctx, tx, repo.Prefix, repo.ID, in.Aliases); err != nil {
			return Repo{}, err
		}
	}
	if err := insertRepoAudit(ctx, tx, RepoAuditInput{
		Prefix:    repo.Prefix,
		RepoID:    repo.ID,
		Action:    "repo.update",
		Actor:     in.Actor,
		OldValues: repoAuditValues(oldRepo),
		NewValues: repoAuditValues(repo),
	}); err != nil {
		return Repo{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Repo{}, fmt.Errorf("store: UpdateRepo commit: %w", err)
	}
	return repo, nil
}

// DisableRepo marks a repo disabled. It is a convenience wrapper over
// UpdateRepo so authorization and audit semantics stay identical.
func (s *Store) DisableRepo(ctx context.Context, prefix, slug, actor string) (Repo, error) {
	enabled := false
	return s.UpdateRepo(ctx, prefix, slug, UpdateRepoInput{
		Enabled: &enabled,
		Actor:   actor,
	})
}

// GetRepoBySlug returns a repo scoped by project prefix and slug.
func (s *Store) GetRepoBySlug(ctx context.Context, prefix, slug string) (Repo, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Repo{}, err
	}
	return getRepoBySlug(ctx, pool, prefix, slug)
}

// ResolveRepoAlias resolves alias to the repo it references.
func (s *Store) ResolveRepoAlias(ctx context.Context, prefix, alias string) (Repo, error) {
	pool, err := s.p.conn()
	if err != nil {
		return Repo{}, err
	}
	row := pool.QueryRow(ctx, `
		SELECT r.id, r.prefix, r.slug, r.display_name, r.remote_url, r.default_branch,
		       r.worktree_subdir, r.clone_strategy, r.auth_ref, r.enabled, r.metadata,
		       r.created_at, r.updated_at, r.created_by, r.updated_by
		FROM bn_repo_aliases a
		JOIN bn_repos r ON r.id = a.repo_id
		WHERE a.prefix = $1 AND a.alias = $2`,
		prefix, alias,
	)
	repo, err := scanRepo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repo{}, fmt.Errorf("store: %w: repo alias %s", ErrNotFound, alias)
	}
	if err != nil {
		return Repo{}, fmt.Errorf("store: ResolveRepoAlias: %w", err)
	}
	return repo, nil
}

// ListRepos returns repos scoped by prefix ordered by slug.
func (s *Store) ListRepos(ctx context.Context, prefix string, includeDisabled bool) ([]Repo, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}
	q := `
		SELECT id, prefix, slug, display_name, remote_url, default_branch,
		       worktree_subdir, clone_strategy, auth_ref, enabled, metadata,
		       created_at, updated_at, created_by, updated_by
		FROM bn_repos
		WHERE prefix = $1`
	if !includeDisabled {
		q += ` AND enabled = true`
	}
	q += ` ORDER BY slug`
	rows, err := pool.Query(ctx, q, prefix)
	if err != nil {
		return nil, fmt.Errorf("store: ListRepos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		repo, err := scanRepo(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListRepos scan: %w", err)
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

// InsertRepoAudit appends one redacted repo audit row.
func (s *Store) InsertRepoAudit(ctx context.Context, in RepoAuditInput) (RepoAudit, error) {
	pool, err := s.p.conn()
	if err != nil {
		return RepoAudit{}, err
	}
	var audit RepoAudit
	var oldValues, newValues []byte
	err = insertRepoAuditReturning(ctx, pool, in).Scan(
		&audit.ID, &audit.Prefix, &audit.RepoID, &audit.Action, &audit.Actor,
		&oldValues, &newValues, &audit.Command, &audit.CreatedAt,
	)
	if err != nil {
		return RepoAudit{}, fmt.Errorf("store: InsertRepoAudit: %w", err)
	}
	audit.OldValues = decodeJSONObject(oldValues)
	audit.NewValues = decodeJSONObject(newValues)
	audit.CreatedAt = audit.CreatedAt.UTC()
	return audit, nil
}

// ListRepoAudit returns recent audit rows for a project, optionally scoped to a
// repo id. limit <= 0 uses a small default.
func (s *Store) ListRepoAudit(ctx context.Context, prefix, repoID string, limit int) ([]RepoAudit, error) {
	pool, err := s.p.conn()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	args := []any{prefix}
	q := `
		SELECT id, prefix, repo_id, action, actor, old_values, new_values, command, created_at
		FROM bn_repo_audit
		WHERE prefix = $1`
	if repoID != "" {
		args = append(args, repoID)
		q += fmt.Sprintf(" AND repo_id = $%d", len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListRepoAudit: %w", err)
	}
	defer rows.Close()

	var audits []RepoAudit
	for rows.Next() {
		var audit RepoAudit
		var oldValues, newValues []byte
		if err := rows.Scan(
			&audit.ID, &audit.Prefix, &audit.RepoID, &audit.Action, &audit.Actor,
			&oldValues, &newValues, &audit.Command, &audit.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: ListRepoAudit scan: %w", err)
		}
		audit.OldValues = decodeJSONObject(oldValues)
		audit.NewValues = decodeJSONObject(newValues)
		audit.CreatedAt = audit.CreatedAt.UTC()
		audits = append(audits, audit)
	}
	return audits, rows.Err()
}

func getRepoBySlug(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, prefix, slug string) (Repo, error) {
	row := q.QueryRow(ctx, `
		SELECT id, prefix, slug, display_name, remote_url, default_branch,
		       worktree_subdir, clone_strategy, auth_ref, enabled, metadata,
		       created_at, updated_at, created_by, updated_by
		FROM bn_repos WHERE prefix = $1 AND slug = $2`,
		prefix, slug,
	)
	repo, err := scanRepo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repo{}, fmt.Errorf("store: %w: repo %s", ErrNotFound, slug)
	}
	if err != nil {
		return Repo{}, fmt.Errorf("store: GetRepoBySlug: %w", err)
	}
	return repo, nil
}

func replaceRepoAliases(ctx context.Context, tx pgx.Tx, prefix, repoID string, aliases []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM bn_repo_aliases WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("store: replaceRepoAliases delete: %w", err)
	}
	cleaned := cleanAliases(aliases)
	for _, alias := range cleaned {
		_, err := tx.Exec(ctx,
			`INSERT INTO bn_repo_aliases (prefix, alias, repo_id) VALUES ($1, $2, $3)`,
			prefix, alias, repoID,
		)
		if err != nil {
			if isDupKeyConflict(err) {
				return fmt.Errorf("store: %w: repo alias %s", ErrConflict, alias)
			}
			return fmt.Errorf("store: replaceRepoAliases insert %s: %w", alias, err)
		}
	}
	return nil
}

func cleanAliases(aliases []string) []string {
	seen := make(map[string]struct{}, len(aliases))
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(strings.ToLower(alias))
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func insertRepoAudit(ctx context.Context, tx pgx.Tx, in RepoAuditInput) error {
	oldValues, err := encodeJSONObject(in.OldValues)
	if err != nil {
		return fmt.Errorf("store: insertRepoAudit old values: %w", err)
	}
	newValues, err := encodeJSONObject(in.NewValues)
	if err != nil {
		return fmt.Errorf("store: insertRepoAudit new values: %w", err)
	}
	var repoID *string
	if in.RepoID != "" {
		repoID = &in.RepoID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO bn_repo_audit
			(prefix, repo_id, action, actor, old_values, new_values, command)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		in.Prefix, repoID, in.Action, in.Actor, oldValues, newValues, in.Command,
	)
	if err != nil {
		return fmt.Errorf("store: insertRepoAudit: %w", err)
	}
	return nil
}

func insertRepoAuditReturning(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, in RepoAuditInput) pgx.Row {
	oldValues, oldErr := encodeJSONObject(in.OldValues)
	newValues, newErr := encodeJSONObject(in.NewValues)
	if oldErr != nil {
		return errorRow{err: oldErr}
	}
	if newErr != nil {
		return errorRow{err: newErr}
	}
	var repoID *string
	if in.RepoID != "" {
		repoID = &in.RepoID
	}
	return q.QueryRow(ctx, `
		INSERT INTO bn_repo_audit
			(prefix, repo_id, action, actor, old_values, new_values, command)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, prefix, repo_id, action, actor, old_values, new_values, command, created_at`,
		in.Prefix, repoID, in.Action, in.Actor, oldValues, newValues, in.Command,
	)
}

func repoAuditValues(repo Repo) map[string]any {
	return map[string]any{
		"id":              repo.ID,
		"slug":            repo.Slug,
		"display_name":    repo.DisplayName,
		"remote_url":      repo.RemoteURL,
		"default_branch":  repo.DefaultBranch,
		"worktree_subdir": repo.WorktreeSubdir,
		"clone_strategy":  repo.CloneStrategy,
		"auth_ref":        repo.AuthRef,
		"enabled":         repo.Enabled,
	}
}

func scanRepo(row pgx.Row) (Repo, error) {
	var (
		repo     Repo
		metadata []byte
	)
	err := row.Scan(
		&repo.ID, &repo.Prefix, &repo.Slug, &repo.DisplayName, &repo.RemoteURL,
		&repo.DefaultBranch, &repo.WorktreeSubdir, &repo.CloneStrategy,
		&repo.AuthRef, &repo.Enabled, &metadata, &repo.CreatedAt, &repo.UpdatedAt,
		&repo.CreatedBy, &repo.UpdatedBy,
	)
	if err != nil {
		return Repo{}, err
	}
	repo.Metadata = decodeJSONObject(metadata)
	repo.CreatedAt = repo.CreatedAt.UTC()
	repo.UpdatedAt = repo.UpdatedAt.UTC()
	return repo, nil
}

func encodeJSONObject(v map[string]any) ([]byte, error) {
	raw, err := jsonObject(v)
	if err != nil {
		return nil, err
	}
	return []byte(raw), nil
}

func decodeJSONObject(raw []byte) map[string]any {
	return objectFromJSON(raw)
}

func generateRepoID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: generate repo id: %w", err)
	}
	return "repo-" + hex.EncodeToString(b[:]), nil
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(...any) error {
	return r.err
}
