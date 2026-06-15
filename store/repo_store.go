package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	db, err := s.p.gorm()
	if err != nil {
		return err
	}
	if strings.TrimSpace(targetActor) == "" {
		return fmt.Errorf("store: AddRepoAdmin: actor must not be empty")
	}

	if bootstrap {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			now := newGORMTime(clockNowUTC())
			claim := gormProjectAdminBootstrap{Prefix: prefix, Actor: targetActor, CreatedAt: now}
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
			if res.Error != nil {
				return fmt.Errorf("store: AddRepoAdmin bootstrap claim: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return ErrUnauthorized
			}
			admin := gormProjectAdmin{Prefix: prefix, Actor: targetActor, CreatedAt: now}
			if err := tx.Create(&admin).Error; err != nil {
				return fmt.Errorf("store: AddRepoAdmin bootstrap: %w", err)
			}
			return nil
		})
	}
	if err := s.AuthorizeRepoAdmin(ctx, prefix, actor); err != nil {
		return err
	}

	admin := gormProjectAdmin{Prefix: prefix, Actor: targetActor, CreatedAt: newGORMTime(clockNowUTC())}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&admin).Error; err != nil {
		return fmt.Errorf("store: AddRepoAdmin: %w", err)
	}
	return nil
}

// ListRepoAdmins returns project admins ordered by actor.
func (s *Store) ListRepoAdmins(ctx context.Context, prefix string) ([]string, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}
	var rows []gormProjectAdmin
	err = db.WithContext(ctx).
		Where("prefix = ?", prefix).
		Order("actor ASC").
		Find(&rows).
		Error
	if err != nil {
		return nil, fmt.Errorf("store: ListRepoAdmins: %w", err)
	}
	admins := make([]string, 0, len(rows))
	for _, row := range rows {
		admins = append(admins, row.Actor)
	}
	return admins, nil
}

// RemoveRepoAdmin removes targetActor from the project admin set. actor must
// already be an admin; this prevents arbitrary BN_DSN holders from stripping
// repo registry ownership.
func (s *Store) RemoveRepoAdmin(ctx context.Context, prefix, targetActor, actor string) error {
	db, err := s.p.gorm()
	if err != nil {
		return err
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRepoAdminBootstrap(ctx, tx, prefix); err != nil {
			return err
		}
		authorized, err := repoAdminExists(ctx, tx, prefix, actor)
		if err != nil {
			return fmt.Errorf("store: RemoveRepoAdmin authorize: %w", err)
		}
		if !authorized {
			return ErrUnauthorized
		}
		var adminCount int64
		if err := tx.Model(&gormProjectAdmin{}).Where("prefix = ?", prefix).Count(&adminCount).Error; err != nil {
			return fmt.Errorf("store: RemoveRepoAdmin count admins: %w", err)
		}
		if adminCount <= 1 && targetActor == actor {
			return fmt.Errorf("store: %w: cannot remove last repo admin", ErrConflict)
		}
		res := tx.Where("prefix = ? AND actor = ?", prefix, targetActor).Delete(&gormProjectAdmin{})
		if res.Error != nil {
			return fmt.Errorf("store: RemoveRepoAdmin: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: %w: repo admin %s", ErrNotFound, targetActor)
		}
		return nil
	})
}

// AuthorizeRepoAdmin returns nil when actor can mutate repo registry state.
func (s *Store) AuthorizeRepoAdmin(ctx context.Context, prefix, actor string) error {
	db, err := s.p.gorm()
	if err != nil {
		return err
	}
	ok, err := repoAdminExists(ctx, db.WithContext(ctx), prefix, actor)
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
	db, err := s.p.gorm()
	if err != nil {
		return Repo{}, err
	}
	if err := s.AuthorizeRepoAdmin(ctx, in.Prefix, in.Actor); err != nil {
		return Repo{}, err
	}

	id, err := generateRepoID()
	if err != nil {
		return Repo{}, err
	}
	displayName := in.DisplayName
	if displayName == "" {
		displayName = in.Slug
	}
	rawURL := strings.TrimSpace(in.RemoteURL)
	remoteURL, err := repo.NormalizeRemoteURL(rawURL)
	if err != nil {
		return Repo{}, fmt.Errorf("store: CreateRepo: %w", err)
	}
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
	now := newGORMTime(clockNowUTC())
	row := gormRepo{
		ID:             id,
		Prefix:         in.Prefix,
		Slug:           in.Slug,
		DisplayName:    displayName,
		RemoteURL:      remoteURL,
		DefaultBranch:  defaultBranch,
		WorktreeSubdir: worktreeSubdir,
		CloneStrategy:  cloneStrategy,
		AuthRef:        authRef,
		Enabled:        true,
		Metadata:       datatypes.JSON(metadata),
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      in.Actor,
		UpdatedBy:      in.Actor,
	}
	repo := repoFromGORM(row)

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			if isDupKeyConflict(err) {
				return fmt.Errorf("store: %w: repo %s", ErrConflict, in.Slug)
			}
			return fmt.Errorf("store: CreateRepo: %w", err)
		}
		if err := replaceRepoAliases(ctx, tx, repo.Prefix, repo.ID, in.Aliases); err != nil {
			return err
		}
		if err := insertRepoAudit(ctx, tx, RepoAuditInput{
			Prefix:    repo.Prefix,
			RepoID:    repo.ID,
			Action:    "repo.create",
			Actor:     in.Actor,
			NewValues: repoAuditValues(repo),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Repo{}, err
	}
	return repo, nil
}

// UpdateRepo applies partial repo updates, optionally replaces aliases, and
// writes an audit row atomically. Repo registry mutation requires admin access.
func (s *Store) UpdateRepo(ctx context.Context, prefix, slug string, in UpdateRepoInput) (Repo, error) {
	db, err := s.p.gorm()
	if err != nil {
		return Repo{}, err
	}
	if err := s.AuthorizeRepoAdmin(ctx, prefix, in.Actor); err != nil {
		return Repo{}, err
	}

	var out Repo
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		oldRepo, err := getRepoBySlugGORM(ctx, tx, prefix, slug)
		if err != nil {
			return err
		}
		target := repo.Target{
			RemoteURL:      oldRepo.RemoteURL,
			DefaultBranch:  oldRepo.DefaultBranch,
			WorktreeSubdir: oldRepo.WorktreeSubdir,
			CloneStrategy:  oldRepo.CloneStrategy,
			AuthRef:        oldRepo.AuthRef,
		}
		if in.RemoteURL != nil {
			norm, nerr := repo.NormalizeRemoteURL(strings.TrimSpace(*in.RemoteURL))
			if nerr != nil {
				return fmt.Errorf("store: UpdateRepo: %w", nerr)
			}
			target.RemoteURL = norm
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
			return fmt.Errorf("store: UpdateRepo validation: %w", err)
		}

		updates := map[string]any{
			"updated_at": newGORMTime(clockNowUTC()),
			"updated_by": in.Actor,
		}
		if in.DisplayName != nil {
			updates["display_name"] = *in.DisplayName
		}
		if in.RemoteURL != nil {
			updates["remote_url"] = target.RemoteURL
		}
		if in.DefaultBranch != nil {
			updates["default_branch"] = target.DefaultBranch
		}
		if in.WorktreeSubdir != nil {
			updates["worktree_subdir"] = target.WorktreeSubdir
		}
		if in.CloneStrategy != nil {
			updates["clone_strategy"] = target.CloneStrategy
		}
		if in.AuthRef != nil {
			updates["auth_ref"] = target.AuthRef
		}
		if in.Enabled != nil {
			updates["enabled"] = *in.Enabled
		}
		if in.Metadata != nil {
			metadata, err := encodeJSONObject(in.Metadata)
			if err != nil {
				return fmt.Errorf("store: UpdateRepo metadata: %w", err)
			}
			updates["metadata"] = datatypes.JSON(metadata)
		}

		if err := tx.Model(&gormRepo{}).Where("id = ?", oldRepo.ID).Updates(updates).Error; err != nil {
			if isDupKeyConflict(err) {
				return fmt.Errorf("store: %w: duplicate remote URL", ErrConflict)
			}
			return fmt.Errorf("store: UpdateRepo: %w", err)
		}
		repo, err := getRepoBySlugGORM(ctx, tx, prefix, slug)
		if err != nil {
			return err
		}
		if in.Aliases != nil {
			if err := replaceRepoAliases(ctx, tx, repo.Prefix, repo.ID, in.Aliases); err != nil {
				return err
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
			return err
		}
		out = repo
		return nil
	})
	if err != nil {
		return Repo{}, err
	}
	return out, nil
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
	db, err := s.p.gorm()
	if err != nil {
		return Repo{}, err
	}
	return getRepoBySlugGORM(ctx, db.WithContext(ctx), prefix, slug)
}

// GetRepoByRemoteURL returns the repo whose stored canonical remote URL matches
// the given URL.  The input is normalized via NormalizeRemoteURL before
// lookup, so any transport form (SCP, SSH URL, HTTPS) that collapses to the
// same canonical key finds the same row.  Returns ErrNotFound when no
// matching repo exists, so callers can fall through to auto-register.
func (s *Store) GetRepoByRemoteURL(ctx context.Context, remoteURL string) (Repo, error) {
	normalized, err := repo.NormalizeRemoteURL(remoteURL)
	if err != nil {
		return Repo{}, fmt.Errorf("store: GetRepoByRemoteURL: %w", err)
	}
	db, err := s.p.gorm()
	if err != nil {
		return Repo{}, err
	}
	return getRepoByRemoteURLGORM(ctx, db.WithContext(ctx), normalized)
}

// ResolveRepoAlias resolves alias to the repo it references.
func (s *Store) ResolveRepoAlias(ctx context.Context, prefix, alias string) (Repo, error) {
	db, err := s.p.gorm()
	if err != nil {
		return Repo{}, err
	}
	var row gormRepo
	err = db.WithContext(ctx).
		Table("bn_repos AS r").
		Select("r.*").
		Joins("JOIN bn_repo_aliases a ON a.repo_id = r.id").
		Where("a.prefix = ? AND a.alias = ?", prefix, alias).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Repo{}, fmt.Errorf("store: %w: repo alias %s", ErrNotFound, alias)
	}
	if err != nil {
		return Repo{}, fmt.Errorf("store: ResolveRepoAlias: %w", err)
	}
	return repoFromGORM(row), nil
}

// ListRepos returns repos scoped by prefix ordered by slug.
func (s *Store) ListRepos(ctx context.Context, prefix string, includeDisabled bool) ([]Repo, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Where("prefix = ?", prefix).Order("slug ASC")
	if !includeDisabled {
		q = q.Where("enabled = ?", true)
	}
	var rows []gormRepo
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: ListRepos: %w", err)
	}
	repos := make([]Repo, 0, len(rows))
	for _, row := range rows {
		repos = append(repos, repoFromGORM(row))
	}
	return repos, nil
}

// InsertRepoAudit appends one redacted repo audit row.
func (s *Store) InsertRepoAudit(ctx context.Context, in RepoAuditInput) (RepoAudit, error) {
	db, err := s.p.gorm()
	if err != nil {
		return RepoAudit{}, err
	}
	row, err := repoAuditRow(in)
	if err != nil {
		return RepoAudit{}, err
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		return RepoAudit{}, fmt.Errorf("store: InsertRepoAudit: %w", err)
	}
	return repoAuditFromGORM(row), nil
}

// ListRepoAudit returns recent audit rows for a project, optionally scoped to a
// repo id. limit <= 0 uses a small default.
func (s *Store) ListRepoAudit(ctx context.Context, prefix, repoID string, limit int) ([]RepoAudit, error) {
	db, err := s.p.gorm()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := db.WithContext(ctx).Where("prefix = ?", prefix)
	if repoID != "" {
		q = q.Where("repo_id = ?", repoID)
	}
	var rows []gormRepoAudit
	if err := q.Order("created_at DESC").Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: ListRepoAudit: %w", err)
	}
	audits := make([]RepoAudit, 0, len(rows))
	for _, row := range rows {
		audits = append(audits, repoAuditFromGORM(row))
	}
	return audits, nil
}

func replaceRepoAliases(ctx context.Context, tx *gorm.DB, prefix, repoID string, aliases []string) error {
	if err := tx.WithContext(ctx).Where("repo_id = ?", repoID).Delete(&gormRepoAlias{}).Error; err != nil {
		return fmt.Errorf("store: replaceRepoAliases delete: %w", err)
	}
	cleaned := cleanAliases(aliases)
	for _, alias := range cleaned {
		row := gormRepoAlias{
			Prefix:    prefix,
			Alias:     alias,
			RepoID:    repoID,
			CreatedAt: newGORMTime(clockNowUTC()),
		}
		if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
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

func repoAdminExists(ctx context.Context, db *gorm.DB, prefix, actor string) (bool, error) {
	var count int64
	err := db.WithContext(ctx).
		Model(&gormProjectAdmin{}).
		Where("prefix = ? AND actor = ?", prefix, actor).
		Count(&count).
		Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func lockRepoAdminBootstrap(ctx context.Context, tx *gorm.DB, prefix string) error {
	var claim gormProjectAdminBootstrap
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("prefix = ?", prefix).
		First(&claim).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("store: %w: missing repo admin bootstrap guard for %s", ErrConflict, prefix)
	}
	if err != nil {
		return fmt.Errorf("store: repo admin bootstrap guard: %w", err)
	}
	return nil
}

func insertRepoAudit(ctx context.Context, tx *gorm.DB, in RepoAuditInput) error {
	row, err := repoAuditRow(in)
	if err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("store: insertRepoAudit: %w", err)
	}
	return nil
}

func repoAuditRow(in RepoAuditInput) (gormRepoAudit, error) {
	oldValues, err := encodeJSONObject(in.OldValues)
	if err != nil {
		return gormRepoAudit{}, fmt.Errorf("store: insertRepoAudit old values: %w", err)
	}
	newValues, err := encodeJSONObject(in.NewValues)
	if err != nil {
		return gormRepoAudit{}, fmt.Errorf("store: insertRepoAudit new values: %w", err)
	}
	var repoID *string
	if in.RepoID != "" {
		repoID = &in.RepoID
	}
	return gormRepoAudit{
		Prefix:    in.Prefix,
		RepoID:    repoID,
		Action:    in.Action,
		Actor:     in.Actor,
		OldValues: datatypes.JSON(oldValues),
		NewValues: datatypes.JSON(newValues),
		Command:   in.Command,
		CreatedAt: newGORMTime(clockNowUTC()),
	}, nil
}

func repoAuditFromGORM(row gormRepoAudit) RepoAudit {
	return RepoAudit{
		ID:        row.ID,
		Prefix:    row.Prefix,
		RepoID:    row.RepoID,
		Action:    row.Action,
		Actor:     row.Actor,
		OldValues: decodeJSONObject(row.OldValues),
		NewValues: decodeJSONObject(row.NewValues),
		Command:   row.Command,
		CreatedAt: row.CreatedAt.UTC(),
	}
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

// ---------------------------------------------------------------------------
// Auto-register (topology-a slug-disambiguation)
// ---------------------------------------------------------------------------

var (
	slugNonAlphanumRE = regexp.MustCompile(`[^a-z0-9._-]+`)
	slugLeadingRE     = regexp.MustCompile(`^[^a-z0-9]+`)
)

// normalizeSlugCandidate applies the Step 0 normalization from the topology-a
// disambiguation algorithm: lowercase → collapse non-slug chars to "-" → strip
// leading non-alphanumeric → strip trailing "-".  Returns "" when the result
// is empty, signalling the caller to skip this candidate.
func normalizeSlugCandidate(s string) string {
	s = strings.ToLower(s)
	s = slugNonAlphanumRE.ReplaceAllString(s, "-")
	s = slugLeadingRE.ReplaceAllString(s, "")
	s = strings.TrimRight(s, "-")
	return s
}

// repoHostShortname derives the single-label shortname from a host string
// (which may include a port) per the topology-a spec:
//
//   - Strip port.
//   - If the result is an IP address or "localhost", use it literally (dots/colons → dashes).
//   - Otherwise, use the first DNS label.
func repoHostShortname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	if net.ParseIP(host) != nil {
		return strings.NewReplacer(".", "-", ":", "-").Replace(host)
	}
	if host == "localhost" {
		return "localhost"
	}
	if idx := strings.IndexByte(host, '.'); idx > 0 {
		return host[:idx]
	}
	return host
}

// parseRepoURLParts extracts the host, owner, and repo-name segments from a
// normalized remote URL (output of NormalizeRemoteURL).  For single-segment
// paths (e.g. file:///tmp/repo), owner is empty.
func parseRepoURLParts(normalized string) (host, owner, repoName string, err error) {
	u, parseErr := url.Parse(normalized)
	if parseErr != nil {
		return "", "", "", fmt.Errorf("parse URL components: %w", parseErr)
	}
	host = u.Host

	path := strings.TrimLeft(u.Path, "/")
	// Defensive: NormalizeRemoteURL already strips .git, but strip again for safety.
	path = strings.TrimSuffix(path, ".git")

	var segs []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	switch len(segs) {
	case 0:
		return host, "", "", fmt.Errorf("URL has no path components: %s", normalized)
	case 1:
		return host, "", segs[0], nil
	default:
		return host, segs[0], segs[len(segs)-1], nil
	}
}

// slugCandidateList returns the ordered candidate prefix strings for the
// four-step slug-disambiguation algorithm (before Step 0 normalization).
//
// Order: step 1 (bare repo name), step 2 (owner-qualified, if owner ≠ ""),
// step 3 (host-qualified), step 4 (numeric suffixes 2-99 on the step-2 or
// step-1 base).
func slugCandidateList(host, owner, repoName string) []string {
	short := repoHostShortname(host)
	candidates := make([]string, 0, 102)

	// Step 1
	candidates = append(candidates, repoName)

	// Step 2 (owner-qualified, only when owner is present)
	var ownerRepo string
	if owner != "" {
		ownerRepo = owner + "-" + repoName
		candidates = append(candidates, ownerRepo)
	}

	// Step 3 (host-qualified)
	if owner != "" {
		candidates = append(candidates, short+"-"+owner+"-"+repoName)
	} else {
		candidates = append(candidates, short+"-"+repoName)
	}

	// Step 4 (numeric suffix; base = step-2 form if owner present, else step-1)
	base := repoName
	if owner != "" {
		base = ownerRepo
	}
	for i := 2; i <= 99; i++ {
		candidates = append(candidates, fmt.Sprintf("%s-%d", base, i))
	}

	return candidates
}

// AutoRegisterInput carries the parameters for a non-interactive repo
// auto-register.
type AutoRegisterInput struct {
	// RemoteURL is the repository's remote URL (any transport form; normalized
	// internally).  For local-only repos with no remote, pass a synthetic
	// file:///abs/git/toplevel URL.
	RemoteURL string
	// DefaultBranch is the default branch name; empty string → "main".
	DefaultBranch string
	// AuthRef is the auth credential reference (e.g. "ssh-key:default").
	// Empty string → repo.AuthRefTestNone ("test:none"), which is valid for
	// local-only repos and CI environments.
	AuthRef string
	// Actor is the entity triggering the registration (e.g. "system" or a
	// username).  Used for audit rows.
	Actor string
}

// AutoRegisterRepo looks up or creates a bn_projects + bn_repos row pair for
// the given remote URL following the topology-a prefix==slug convention (see
// docs/specs/topology-a-prefix-equals-slug.md).
//
// If a repo already exists for the normalized URL, it is returned immediately
// (idempotent by canonical URL).  Otherwise the four-step slug-disambiguation
// algorithm is run to find a free prefix, then the project and repo are
// created atomically in one transaction.
//
// Returns ErrSlugExhausted if all candidates remain taken after 5 full-sequence
// retries.
func (s *Store) AutoRegisterRepo(ctx context.Context, in AutoRegisterInput) (Repo, error) {
	// Normalize the URL to the canonical form used as the idempotency key.
	normalized, err := repo.NormalizeRemoteURL(in.RemoteURL)
	if err != nil {
		return Repo{}, fmt.Errorf("store: AutoRegisterRepo: %w", err)
	}

	// Idempotency: return existing row immediately if the URL is already registered.
	existing, err := s.GetRepoByRemoteURL(ctx, normalized)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Repo{}, err
	}

	db, err := s.p.gorm()
	if err != nil {
		return Repo{}, err
	}

	authRef := strings.TrimSpace(in.AuthRef)
	if authRef == "" {
		authRef = repo.AuthRefTestNone
	}
	defaultBranch := repo.NormalizeDefaultBranch(in.DefaultBranch)
	actor := strings.TrimSpace(in.Actor)

	// Parse normalized URL into host/owner/repo-name components.
	host, owner, repoName, err := parseRepoURLParts(normalized)
	if err != nil {
		return Repo{}, fmt.Errorf("store: AutoRegisterRepo: %w", err)
	}

	candidates := slugCandidateList(host, owner, repoName)

	const maxRetries = 5
	for range maxRetries {
		for _, raw := range candidates {
			slug := normalizeSlugCandidate(raw)
			if slug == "" {
				continue
			}

			// Pre-check outside tx; check-then-act race is absorbed by ErrConflict retry.
			exists, err := s.ProjectExists(ctx, slug)
			if err != nil {
				return Repo{}, fmt.Errorf("store: AutoRegisterRepo: %w", err)
			}
			if exists {
				continue
			}

			result, err := s.autoRegisterTx(ctx, db, slug, normalized, defaultBranch, authRef, actor)
			if err == nil {
				return result, nil
			}
			if errors.Is(err, ErrConflict) {
				// Concurrent registration claimed this slug; try the next candidate.
				continue
			}
			return Repo{}, err
		}
	}

	return Repo{}, fmt.Errorf("store: AutoRegisterRepo: %w", ErrSlugExhausted)
}

// autoRegisterTx creates a bn_projects row (idempotent via OnConflict DoNothing)
// and a bn_repos row inside a single transaction.  Returns ErrConflict when
// either unique constraint fires, allowing the caller to advance to the next
// slug candidate.
func (s *Store) autoRegisterTx(ctx context.Context, db *gorm.DB, slug, normalizedURL, defaultBranch, authRef, actor string) (Repo, error) {
	id, err := generateRepoID()
	if err != nil {
		return Repo{}, err
	}

	metadata, encErr := encodeJSONObject(nil)
	if encErr != nil {
		return Repo{}, fmt.Errorf("store: autoRegisterTx: %w", encErr)
	}
	now := newGORMTime(clockNowUTC())

	row := gormRepo{
		ID:            id,
		Prefix:        slug,
		Slug:          slug,
		DisplayName:   slug,
		RemoteURL:     normalizedURL,
		DefaultBranch: defaultBranch,
		CloneStrategy: repo.NormalizeCloneStrategy(""),
		AuthRef:       authRef,
		Enabled:       true,
		Metadata:      datatypes.JSON(metadata),
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     actor,
		UpdatedBy:     actor,
	}

	var result Repo
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		projErr := tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&gormProject{Prefix: slug, CreatedAt: now}).
			Error
		if projErr != nil {
			if isDupKeyConflict(projErr) {
				return fmt.Errorf("store: %w: project prefix %s", ErrConflict, slug)
			}
			return fmt.Errorf("store: autoRegisterTx: %w", projErr)
		}

		if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
			if isDupKeyConflict(err) {
				return fmt.Errorf("store: %w: repo slug or remote URL %s", ErrConflict, slug)
			}
			return fmt.Errorf("store: autoRegisterTx: %w", err)
		}

		result = repoFromGORM(row)
		return nil
	})
	if txErr != nil {
		return Repo{}, txErr
	}
	return result, nil
}
