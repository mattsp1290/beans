package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

type appState struct {
	// resolved from flags / env
	prefix  string
	actor   string
	jsonOut bool

	// lazily set by initConn
	store *store.Store
}

func newRootCmd(rs *appState) *cobra.Command {
	root := &cobra.Command{
		Use:           "bn",
		Short:         "Database-backed issue tracker (bn = beans)",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return rs.initConn(cmd.Context())
		},
	}

	root.PersistentFlags().StringVar(&rs.prefix, "project", "", "project prefix (overrides $BN_PROJECT)")
	root.PersistentFlags().StringVar(&rs.actor, "actor", "", "audit actor (overrides $BN_ACTOR)")
	root.PersistentFlags().BoolVar(&rs.jsonOut, "json", false, "machine-readable JSON output")

	root.AddCommand(
		newInitCmd(rs),
		newCreateCmd(rs),
		newReadyCmd(rs),
		newListCmd(rs),
		newShowCmd(rs),
		newUpdateCmd(rs),
		newCloseCmd(rs),
		newDeleteCmd(rs),
		newDepCmd(rs),
		newRepoCmd(rs),
		newExportCmd(rs),
		newImportCmd(rs),
		newRememberCmd(rs),
		newMemoriesCmd(rs),
		newPrimeCmd(),
	)
	return root
}

// initConn opens the configured store and resolves actor + prefix from env.
func (rs *appState) initConn(ctx context.Context) error {
	return rs.initConnWithOptions(ctx, false)
}

func (rs *appState) initConnForInit(ctx context.Context) error {
	return rs.initConnWithOptions(ctx, true)
}

func (rs *appState) initConnWithOptions(ctx context.Context, skipMarker bool) error {
	cfg, err := storeConfigFromEnv()
	if err != nil {
		return err
	}

	if rs.actor == "" {
		if v := os.Getenv("BN_ACTOR"); v != "" {
			rs.actor = v
		}
	}
	if rs.actor == "" {
		if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" {
				rs.actor = name
			}
		}
	}
	if rs.actor == "" {
		rs.actor = os.Getenv("USER")
	}

	prefix, err := resolveProjectPrefix(rs.prefix, skipMarker)
	if err != nil {
		return err
	}
	rs.prefix = prefix

	st, err := store.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("bn: connect: %w", err)
	}
	rs.store = st
	return nil
}

func storeConfigFromEnv() (store.Config, error) {
	dsn := strings.TrimSpace(os.Getenv("BN_DSN"))
	driverEnv := strings.TrimSpace(os.Getenv("BN_DRIVER"))
	if dsn == "" {
		if driverEnv == "" {
			return store.Config{}, fmt.Errorf("BN_DRIVER and BN_DSN are not set; set BN_DRIVER=postgres|mysql|sqlite and a driver-specific BN_DSN")
		}
		return store.Config{}, fmt.Errorf("BN_DSN is not set; set BN_DSN to the %s connection string", driverEnv)
	}

	driver, err := resolveStoreDriver(driverEnv, dsn)
	if err != nil {
		return store.Config{}, err
	}
	return store.Config{
		Driver: driver,
		DSN:    store.SecretDSN(dsn),
	}, nil
}

func resolveStoreDriver(driverEnv, dsn string) (store.Driver, error) {
	switch strings.ToLower(strings.TrimSpace(driverEnv)) {
	case "postgres", "postgresql", "pg":
		return store.DriverPostgres, nil
	case "mysql":
		return store.DriverMySQL, nil
	case "sqlite", "sqlite3":
		return store.DriverSQLite, nil
	case "":
		if isPostgresDSN(dsn) {
			return store.DriverPostgres, nil
		}
		return "", fmt.Errorf("BN_DRIVER is not set; set BN_DRIVER=postgres, BN_DRIVER=mysql, or BN_DRIVER=sqlite for this BN_DSN")
	default:
		return "", fmt.Errorf("%w: %s", store.ErrUnsupportedDriver, driverEnv)
	}
}

func isPostgresDSN(dsn string) bool {
	_, err := pgxpool.ParseConfig(dsn)
	return err == nil
}

// requirePrefix returns an error when no project prefix is configured.
func (rs *appState) requirePrefix() error {
	if rs.prefix == "" {
		return fmt.Errorf("project prefix required: use --project, set BN_PROJECT, or run bn init --prefix=<project>")
	}
	return nil
}

const activeProjectMarker = ".bn"

func resolveProjectPrefix(flagPrefix string, skipMarker bool) (string, error) {
	if flagPrefix != "" {
		return flagPrefix, nil
	}
	if prefix := os.Getenv("BN_PROJECT"); prefix != "" {
		return prefix, nil
	}
	if skipMarker {
		return "", nil
	}
	return readActiveProjectMarker("")
}

func readActiveProjectMarker(start string) (string, error) {
	cfg, err := readActiveProjectConfig(start)
	if err != nil {
		return "", err
	}
	return cfg.Project, nil
}

func readActiveProjectConfig(start string) (activeProjectConfig, error) {
	path, ok, err := activeProjectMarkerPath(start)
	if err != nil || !ok {
		return activeProjectConfig{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return activeProjectConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	cfg, err := parseActiveProjectConfig(string(raw))
	if err != nil {
		return activeProjectConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}

func writeActiveProjectMarker(prefix string) error {
	if err := validateActiveProjectPrefix(prefix); err != nil {
		return err
	}
	return writeActiveProjectConfig(activeProjectConfig{Project: prefix})
}

func writeActiveRepoMarker(prefix, repo, remote string) error {
	if err := validateActiveProjectPrefix(prefix); err != nil {
		return err
	}
	if repo == "" {
		return fmt.Errorf("repo is empty")
	}
	return writeActiveProjectConfig(activeProjectConfig{
		Project: prefix,
		Repo:    repo,
		Remote:  remote,
	})
}

func writeActiveProjectConfig(cfg activeProjectConfig) error {
	root, err := markerRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, activeProjectMarker)
	var body strings.Builder
	fmt.Fprintf(&body, "project=%s\n", cfg.Project)
	if cfg.Repo != "" {
		fmt.Fprintf(&body, "repo=%s\n", cfg.Repo)
	}
	if cfg.Remote != "" {
		fmt.Fprintf(&body, "remote=%s\n", cfg.Remote)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func validateActiveProjectPrefix(prefix string) error {
	switch {
	case prefix == "":
		return fmt.Errorf("project prefix is empty")
	case strings.TrimSpace(prefix) != prefix:
		return fmt.Errorf("project prefix must not have leading or trailing whitespace")
	case strings.ContainsAny(prefix, "\r\n"):
		return fmt.Errorf("project prefix must not contain newlines")
	default:
		return nil
	}
}

func activeProjectMarkerPath(start string) (string, bool, error) {
	root, ok, err := gitRoot(start)
	if err != nil {
		return "", false, err
	}
	if ok {
		path := filepath.Join(root, activeProjectMarker)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			return path, true, nil
		case err == nil:
			return "", false, fmt.Errorf("%s is a directory; expected active project marker file", path)
		case os.IsNotExist(err):
			return "", false, nil
		default:
			return "", false, fmt.Errorf("stat %s: %w", path, err)
		}
	}
	return findActiveProjectMarker(start)
}

func findActiveProjectMarker(start string) (string, bool, error) {
	if start == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", false, fmt.Errorf("get working directory: %w", err)
		}
		start = wd
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", start, err)
	}
	for {
		path := filepath.Join(dir, activeProjectMarker)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			return path, true, nil
		case err == nil:
			return "", false, fmt.Errorf("%s is a directory; expected active project marker file", path)
		case !os.IsNotExist(err):
			return "", false, fmt.Errorf("stat %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

func parseActiveProjectMarker(raw string) (string, error) {
	cfg, err := parseActiveProjectConfig(raw)
	if err != nil {
		return "", err
	}
	return cfg.Project, nil
}

type activeProjectConfig struct {
	Project string
	Repo    string
	Remote  string
}

func parseActiveProjectConfig(raw string) (activeProjectConfig, error) {
	var cfg activeProjectConfig
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return activeProjectConfig{}, fmt.Errorf("invalid marker line %q", line)
		}
		switch strings.TrimSpace(key) {
		case "project":
			cfg.Project = strings.TrimSpace(value)
		case "repo":
			cfg.Repo = strings.TrimSpace(value)
		case "remote":
			cfg.Remote = strings.TrimSpace(value)
		default:
			continue
		}
	}
	if cfg.Project == "" {
		return activeProjectConfig{}, fmt.Errorf("project is missing")
	}
	return cfg, nil
}

func markerRoot() (string, error) {
	root, ok, err := gitRoot("")
	if err != nil {
		return "", err
	}
	if ok {
		return root, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return wd, nil
}

func gitRoot(start string) (string, bool, error) {
	args := []string{"rev-parse", "--show-toplevel"}
	cmd := exec.Command("git", args...)
	if start != "" {
		cmd.Dir = start
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false, nil
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false, nil
	}
	return root, true, nil
}

// ---------------------------------------------------------------------------
// JSON output helpers
// ---------------------------------------------------------------------------

// issueJSON is the stable CLI JSON shape (bd-compat field names).
type issueJSON struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Priority    int             `json:"priority"`
	Type        string          `json:"type"`
	Labels      []string        `json:"labels"`
	Description string          `json:"description,omitempty"`
	BranchName  string          `json:"branch_name,omitempty"`
	URL         string          `json:"url,omitempty"`
	BlockedBy   []string        `json:"blocked_by,omitempty"`
	Repo        *repoTargetJSON `json:"repo,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type repoTargetJSON struct {
	ID             string         `json:"id"`
	Slug           string         `json:"slug"`
	RemoteURL      string         `json:"remote_url"`
	DefaultBranch  string         `json:"default_branch"`
	RequestedRef   string         `json:"requested_ref,omitempty"`
	BaseRef        string         `json:"base_ref,omitempty"`
	WorkBranch     string         `json:"work_branch,omitempty"`
	WorktreeSubdir string         `json:"worktree_subdir,omitempty"`
	CloneStrategy  string         `json:"clone_strategy"`
	AuthRef        string         `json:"auth_ref,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func toIssueJSON(iss store.Issue) issueJSON {
	labels := iss.Labels
	if labels == nil {
		labels = []string{}
	}
	blocked := iss.BlockedBy
	if blocked == nil {
		blocked = []string{}
	}
	// model.Priority is 1-indexed (PriorityCritical=1); output 0-indexed for bd compat.
	// The clamp to 0 handles PriorityUnset (core 0 → output -1) by emitting 0 (critical).
	// This is latent only: the store's CHECK (priority BETWEEN 0 AND 4) with DEFAULT 2
	// ensures all reads produce a concrete priority, so PriorityUnset is not reachable today.
	prio := int(iss.Priority) - 1
	if prio < 0 {
		prio = 0
	}
	out := issueJSON{
		ID:          iss.ID,
		Title:       iss.Title,
		Status:      string(iss.State),
		Priority:    prio,
		Type:        iss.IssueType,
		Labels:      labels,
		Description: iss.Description,
		BranchName:  iss.BranchName,
		URL:         iss.URL,
		BlockedBy:   blocked,
		CreatedAt:   iss.CreatedAt,
		UpdatedAt:   iss.UpdatedAt,
	}
	if iss.Repo != nil {
		out.Repo = &repoTargetJSON{
			ID:             iss.Repo.ID,
			Slug:           iss.Repo.Slug,
			RemoteURL:      iss.Repo.RemoteURL,
			DefaultBranch:  iss.Repo.DefaultBranch,
			RequestedRef:   iss.Repo.RequestedRef,
			BaseRef:        iss.Repo.BaseRef,
			WorkBranch:     iss.Repo.WorkBranch,
			WorktreeSubdir: iss.Repo.WorktreeSubdir,
			CloneStrategy:  iss.Repo.CloneStrategy,
			AuthRef:        iss.Repo.AuthRef,
			Metadata:       iss.Repo.Metadata,
		}
	}
	return out
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ---------------------------------------------------------------------------
// Table output helpers
// ---------------------------------------------------------------------------

// priorityLabel maps model.Priority to the P-prefixed label used in output.
func priorityLabel(p model.Priority) string {
	switch p {
	case model.PriorityCritical:
		return "P0"
	case model.PriorityHigh:
		return "P1"
	case model.PriorityMedium:
		return "P2"
	case model.PriorityLow:
		return "P3"
	case model.PriorityBacklog:
		return "P4"
	default:
		return "  "
	}
}
