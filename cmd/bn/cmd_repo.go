package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	repovalidation "github.com/mattsp1290/beans/repo"
	store "github.com/mattsp1290/beans/store"
)

var repoSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func newRepoCmd(rs *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage onboarded repositories",
	}
	cmd.AddCommand(
		newRepoAdminCmd(rs),
		newRepoAddCmd(rs),
		newRepoListCmd(rs),
		newRepoShowCmd(rs),
		newRepoDoctorCmd(rs),
		newRepoUpdateCmd(rs),
		newRepoRemoveCmd(rs),
	)
	return cmd
}

func newRepoAdminCmd(rs *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage repo registry admins",
	}
	cmd.AddCommand(
		newRepoAdminAddCmd(rs),
		newRepoAdminListCmd(rs),
		newRepoAdminRemoveCmd(rs),
	)
	return cmd
}

func newRepoAdminAddCmd(rs *appState) *cobra.Command {
	var bootstrap bool
	cmd := &cobra.Command{
		Use:   "add <actor>",
		Short: "Add a repo registry admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			actor := strings.TrimSpace(args[0])
			if actor == "" {
				return fmt.Errorf("actor must not be empty")
			}
			if err := rs.store.EnsureProject(cmd.Context(), rs.prefix); err != nil {
				return fmt.Errorf("repo admin add: ensure project: %w", err)
			}
			if err := rs.store.AddRepoAdmin(cmd.Context(), rs.prefix, actor, rs.actor, bootstrap); err != nil {
				return repoErr("repo admin add", err)
			}
			if rs.jsonOut {
				return writeJSON(map[string]string{"actor": actor})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added repo admin %s\n", actor)
			return nil
		},
	}
	cmd.Flags().BoolVar(&bootstrap, "bootstrap", false, "allow only when the project has no repo admins")
	return cmd
}

func newRepoAdminListCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List repo registry admins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			admins, err := rs.store.ListRepoAdmins(cmd.Context(), rs.prefix)
			if err != nil {
				return repoErr("repo admin list", err)
			}
			if rs.jsonOut {
				return writeJSON(map[string][]string{"admins": admins})
			}
			if len(admins) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No repo admins.")
				return nil
			}
			for _, admin := range admins {
				fmt.Fprintln(cmd.OutOrStdout(), admin)
			}
			return nil
		},
	}
}

func newRepoAdminRemoveCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <actor>",
		Short: "Remove a repo registry admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			actor := strings.TrimSpace(args[0])
			if actor == "" {
				return fmt.Errorf("actor must not be empty")
			}
			if err := rs.store.RemoveRepoAdmin(cmd.Context(), rs.prefix, actor, rs.actor); err != nil {
				return repoErr("repo admin remove", err)
			}
			if rs.jsonOut {
				return writeJSON(map[string]string{"actor": actor})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed repo admin %s\n", actor)
			return nil
		},
	}
}

func newRepoAddCmd(rs *appState) *cobra.Command {
	var (
		remote        string
		displayName   string
		defaultBranch string
		subdir        string
		cloneStrategy string
		authRef       string
		aliases       []string
	)
	cmd := &cobra.Command{
		Use:   "add <slug>",
		Short: "Onboard a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			slug, err := cleanRepoSlug(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(remote) == "" {
				return fmt.Errorf("--remote is required")
			}
			if strings.TrimSpace(authRef) == "" {
				return fmt.Errorf("--auth is required")
			}
			target := repovalidation.Target{
				RemoteURL:      strings.TrimSpace(remote),
				DefaultBranch:  defaultBranch,
				WorktreeSubdir: subdir,
				CloneStrategy:  cloneStrategy,
				AuthRef:        strings.TrimSpace(authRef),
			}
			if err := repovalidation.ValidateTarget(target); err != nil {
				return fmt.Errorf("repo add: validation: %w", err)
			}
			if err := rs.store.EnsureProject(cmd.Context(), rs.prefix); err != nil {
				return fmt.Errorf("repo add: ensure project: %w", err)
			}
			repo, err := rs.store.CreateRepo(cmd.Context(), store.CreateRepoInput{
				Prefix:         rs.prefix,
				Slug:           slug,
				DisplayName:    displayName,
				RemoteURL:      strings.TrimSpace(remote),
				DefaultBranch:  defaultBranch,
				WorktreeSubdir: subdir,
				CloneStrategy:  cloneStrategy,
				AuthRef:        strings.TrimSpace(authRef),
				Actor:          rs.actor,
				Aliases:        append(aliases, slug),
			})
			if err != nil {
				return repoErr("repo add", err)
			}
			if err := writeActiveRepoMarker(rs.prefix, repo.Slug, repo.RemoteURL); err != nil {
				return fmt.Errorf("repo add: write marker: %w", err)
			}
			if rs.jsonOut {
				return writeJSON(repoToJSON(repo))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added repo %s: %s\n", repo.Slug, repo.RemoteURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "git remote URL (required)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name (default: slug)")
	cmd.Flags().StringVar(&defaultBranch, "default-branch", "", "default branch (default: main)")
	cmd.Flags().StringVar(&subdir, "subdir", "", "default worktree subdirectory")
	cmd.Flags().StringVar(&cloneStrategy, "clone-strategy", "", "checkout strategy (default: mirror-cache)")
	cmd.Flags().StringVar(&authRef, "auth", "", "logical auth reference, e.g. ssh-key:github-default (required)")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, "alias for repo resolution (repeatable)")
	return cmd
}

func newRepoDoctorCmd(rs *appState) *cobra.Command {
	var fromOrchestrator bool
	var allowedHosts []string
	cmd := &cobra.Command{
		Use:   "doctor <slug>",
		Short: "Preflight an onboarded repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			if fromOrchestrator {
				return fmt.Errorf("repo doctor: --from-orchestrator is not implemented yet")
			}
			slug, err := cleanRepoSlug(args[0])
			if err != nil {
				return err
			}
			repo, err := rs.store.GetRepoBySlug(cmd.Context(), rs.prefix, slug)
			if err != nil {
				return repoErr("repo doctor", err)
			}
			if !repo.Enabled {
				return fmt.Errorf("repo doctor: repo %s is disabled", repo.Slug)
			}
			target := repovalidation.Target{
				RemoteURL:      repo.RemoteURL,
				DefaultBranch:  repo.DefaultBranch,
				WorktreeSubdir: repo.WorktreeSubdir,
				CloneStrategy:  repo.CloneStrategy,
				AuthRef:        repo.AuthRef,
			}
			if err := repovalidation.ValidateTarget(target); err != nil {
				return fmt.Errorf("repo doctor: validation: %w", err)
			}
			if err := repovalidation.ValidateRemoteAllowed(repo.RemoteURL, allowedHosts); err != nil {
				return fmt.Errorf("repo doctor: host policy: %w", err)
			}
			if err := gitLSRemote(cmd.Context(), repo.RemoteURL); err != nil {
				return fmt.Errorf("repo doctor: git ls-remote: %w", err)
			}
			if rs.jsonOut {
				return writeJSON(map[string]any{
					"slug":    repo.Slug,
					"enabled": repo.Enabled,
					"remote":  repo.RemoteURL,
					"ok":      true,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Repo %s OK\n", repo.Slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromOrchestrator, "from-orchestrator", false, "run checks from the orchestrator host (not implemented)")
	cmd.Flags().StringArrayVar(&allowedHosts, "allowed-host", nil, "allowed git remote host for this check (repeatable)")
	return cmd
}

func newRepoListCmd(rs *appState) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List onboarded repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			repos, err := rs.store.ListRepos(cmd.Context(), rs.prefix, all)
			if err != nil {
				return repoErr("repo list", err)
			}
			if rs.jsonOut {
				out := make([]repoJSON, len(repos))
				for i, repo := range repos {
					out[i] = repoToJSON(repo)
				}
				return writeJSON(out)
			}
			printRepoTable(cmd, repos)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include disabled repositories")
	return cmd
}

func newRepoShowCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "Show repository details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			slug, err := cleanRepoSlug(args[0])
			if err != nil {
				return err
			}
			repo, err := rs.store.GetRepoBySlug(cmd.Context(), rs.prefix, slug)
			if err != nil {
				return repoErr("repo show", err)
			}
			if rs.jsonOut {
				return writeJSON(repoToJSON(repo))
			}
			printRepoDetail(cmd, repo)
			return nil
		},
	}
}

func newRepoUpdateCmd(rs *appState) *cobra.Command {
	var (
		displayName   string
		remote        string
		defaultBranch string
		subdir        string
		cloneStrategy string
		authRef       string
		enable        bool
		disable       bool
		aliases       []string
	)
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update repository configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			if enable && disable {
				return fmt.Errorf("--enable and --disable are mutually exclusive")
			}
			slug, err := cleanRepoSlug(args[0])
			if err != nil {
				return err
			}
			in := store.UpdateRepoInput{Actor: rs.actor}
			if cmd.Flags().Changed("display-name") {
				in.DisplayName = &displayName
			}
			if cmd.Flags().Changed("remote") {
				remote = strings.TrimSpace(remote)
				in.RemoteURL = &remote
			}
			if cmd.Flags().Changed("default-branch") {
				in.DefaultBranch = &defaultBranch
			}
			if cmd.Flags().Changed("subdir") {
				in.WorktreeSubdir = &subdir
			}
			if cmd.Flags().Changed("clone-strategy") {
				in.CloneStrategy = &cloneStrategy
			}
			if cmd.Flags().Changed("auth") {
				authRef = strings.TrimSpace(authRef)
				in.AuthRef = &authRef
			}
			if enable || disable {
				enabled := enable
				in.Enabled = &enabled
			}
			if cmd.Flags().Changed("alias") {
				in.Aliases = aliases
			}
			repo, err := rs.store.UpdateRepo(cmd.Context(), rs.prefix, slug, in)
			if err != nil {
				return repoErr("repo update", err)
			}
			if rs.jsonOut {
				return writeJSON(repoToJSON(repo))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated repo %s\n", repo.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "set display name")
	cmd.Flags().StringVar(&remote, "remote", "", "set git remote URL")
	cmd.Flags().StringVar(&defaultBranch, "default-branch", "", "set default branch")
	cmd.Flags().StringVar(&subdir, "subdir", "", "set default worktree subdirectory")
	cmd.Flags().StringVar(&cloneStrategy, "clone-strategy", "", "set checkout strategy")
	cmd.Flags().StringVar(&authRef, "auth", "", "set logical auth reference")
	cmd.Flags().BoolVar(&enable, "enable", false, "enable repository")
	cmd.Flags().BoolVar(&disable, "disable", false, "disable repository")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, "replace aliases (repeatable)")
	return cmd
}

func newRepoRemoveCmd(rs *appState) *cobra.Command {
	var (
		purge bool
		force bool
	)
	cmd := &cobra.Command{
		Use:   "remove <slug>",
		Short: "Disable (or purge) a repository",
		Long: `Disable a repository so it no longer participates in issue routing.

Without --purge, the repo row is marked disabled (enabled=false) and all its
issues and data are preserved. The repo can be re-enabled with repo update --enable.

With --purge, the entire project is hard-deleted: the bn_projects row is removed
and all associated data (repos, aliases, admins, memories) is cascade-deleted.
Issues belonging to the project are also deleted. If the project has any issues,
--purge requires --force to confirm the data loss.

Under per-repo topology (prefix == slug), --purge removes the repo completely.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			slug, err := cleanRepoSlug(args[0])
			if err != nil {
				return err
			}
			if purge {
				// Under per-repo topology prefix == slug, so the project prefix
				// to purge is rs.prefix (set from the repo whose slug matches).
				if err := rs.store.DeleteProject(cmd.Context(), rs.prefix, force); err != nil {
					return repoErr("repo remove --purge", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Purged project %s\n", rs.prefix)
				return nil
			}
			repo, err := rs.store.DisableRepo(cmd.Context(), rs.prefix, slug, rs.actor)
			if err != nil {
				return repoErr("repo remove", err)
			}
			if rs.jsonOut {
				return writeJSON(repoToJSON(repo))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Disabled repo %s\n", repo.Slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "hard-delete the project and all its data (irreversible)")
	cmd.Flags().BoolVar(&force, "force", false, "with --purge, delete even if the project has issues")
	return cmd
}

func cleanRepoSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	if slug != raw || slug != strings.ToLower(slug) || !repoSlugRE.MatchString(slug) {
		return "", fmt.Errorf("invalid repo slug %q (must match %s)", raw, repoSlugRE.String())
	}
	return slug, nil
}

func repoErr(op string, err error) error {
	switch {
	case errors.Is(err, store.ErrUnauthorized):
		return fmt.Errorf("%s: unauthorized", op)
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("%s: not found", op)
	case errors.Is(err, store.ErrConflict):
		return fmt.Errorf("%s: conflict: %w", op, err)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

func printRepoTable(cmd *cobra.Command, repos []store.Repo) {
	if len(repos) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No repos.")
		return
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-20s  %-8s  %-16s  %s\n", "SLUG", "ENABLED", "BRANCH", "REMOTE")
	fmt.Fprintf(w, "%-20s  %-8s  %-16s  %s\n", "────────────────────", "────────", "────────────────", "─────────────────────────────")
	for _, repo := range repos {
		fmt.Fprintf(w, "%-20s  %-8t  %-16s  %s\n", repo.Slug, repo.Enabled, repo.DefaultBranch, repo.RemoteURL)
	}
}

func printRepoDetail(cmd *cobra.Command, repo store.Repo) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  [%t]\n", repo.Slug, repo.Enabled)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	fmt.Fprintf(w, "ID:             %s\n", repo.ID)
	fmt.Fprintf(w, "Display name:   %s\n", repo.DisplayName)
	fmt.Fprintf(w, "Remote:         %s\n", repo.RemoteURL)
	fmt.Fprintf(w, "Default branch: %s\n", repo.DefaultBranch)
	fmt.Fprintf(w, "Subdir:         %s\n", repo.WorktreeSubdir)
	fmt.Fprintf(w, "Clone strategy: %s\n", repo.CloneStrategy)
	fmt.Fprintf(w, "Auth ref:       %s\n", repo.AuthRef)
	fmt.Fprintf(w, "Created:        %s\n", repo.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Updated:        %s\n", repo.UpdatedAt.Format("2006-01-02 15:04:05"))
}

type repoJSON struct {
	ID             string         `json:"id"`
	Prefix         string         `json:"prefix"`
	Slug           string         `json:"slug"`
	DisplayName    string         `json:"display_name"`
	RemoteURL      string         `json:"remote_url"`
	DefaultBranch  string         `json:"default_branch"`
	WorktreeSubdir string         `json:"worktree_subdir"`
	CloneStrategy  string         `json:"clone_strategy"`
	AuthRef        string         `json:"auth_ref"`
	Enabled        bool           `json:"enabled"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	CreatedBy      string         `json:"created_by"`
	UpdatedBy      string         `json:"updated_by"`
}

func repoToJSON(repo store.Repo) repoJSON {
	metadata := repo.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return repoJSON{
		ID:             repo.ID,
		Prefix:         repo.Prefix,
		Slug:           repo.Slug,
		DisplayName:    repo.DisplayName,
		RemoteURL:      repo.RemoteURL,
		DefaultBranch:  repo.DefaultBranch,
		WorktreeSubdir: repo.WorktreeSubdir,
		CloneStrategy:  repo.CloneStrategy,
		AuthRef:        repo.AuthRef,
		Enabled:        repo.Enabled,
		Metadata:       metadata,
		CreatedAt:      repo.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      repo.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		CreatedBy:      repo.CreatedBy,
		UpdatedBy:      repo.UpdatedBy,
	}
}

func gitLSRemote(ctx context.Context, remote string) error {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", remote)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}
