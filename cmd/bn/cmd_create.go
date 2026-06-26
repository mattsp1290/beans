package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newCreateCmd(rs *appState) *cobra.Command {
	var (
		description    string
		priority       int
		labels         []string
		issueType      string
		repoSlug       string
		requestedRef   string
		worktreeSubdir string
		silent         bool
	)

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			title := strings.TrimSpace(args[0])
			if title == "" {
				return fmt.Errorf("title must not be empty")
			}
			if len(title) > 500 {
				return fmt.Errorf("title must be under 500 characters")
			}
			if priority < 0 || priority > 4 {
				return fmt.Errorf("priority must be 0–4 (0=critical, 4=backlog)")
			}
			repoSlug = strings.TrimSpace(repoSlug)
			if repoSlug == "" {
				cfg, err := readActiveProjectConfig("")
				if err != nil {
					return fmt.Errorf("create: read active repo marker: %w", err)
				}
				repoSlug = cfg.Repo
			}
			// Validate user-supplied slug before falling through to auto-detect.
			if repoSlug != "" {
				var err error
				repoSlug, err = cleanRepoSlug(repoSlug)
				if err != nil {
					return err
				}
			}
			var selectedRepo *store.Repo
			if repoSlug != "" {
				repo, err := rs.store.GetRepoBySlug(cmd.Context(), rs.prefix, repoSlug)
				if err != nil {
					return fmt.Errorf("create: repo %q: %w", repoSlug, err)
				}
				selectedRepo = &repo
			}
			// Git auto-detect: if inside a git repo with no explicit --repo, auto-register
			// and record the issue against the detected repo. The global --repo flag (if set
			// as a URL or slug before the subcommand) is also resolved here.
			if repoSlug == "" {
				repo, err := rs.resolveRepoContext(cmd.Context())
				if err != nil {
					return fmt.Errorf("create: %w", err)
				}
				// Guard: only apply the repo if its prefix matches the current project.
				// CreateIssue looks up the repo by (prefix, slug); a mismatch means the
				// cwd repo belongs to a different project than --project/BN_PROJECT, and
				// silently linking to it would produce a misleading "repo not found" error.
				if repo != nil && repo.Prefix == rs.prefix {
					repoSlug = repo.Slug
					selectedRepo = repo
				}
			}
			if repoSlug == "" && (strings.TrimSpace(requestedRef) != "" || strings.TrimSpace(worktreeSubdir) != "") {
				return fmt.Errorf("--repo is required when --ref or --subdir is set outside an activated repo")
			}

			// D8: auto-register the project so bn init is not required before create.
			if err := rs.store.EnsureProject(cmd.Context(), rs.prefix); err != nil {
				return fmt.Errorf("create: ensure project: %w", err)
			}

			var repoInput *store.IssueRepoInput
			if repoSlug != "" {
				repoInput = &store.IssueRepoInput{
					RepoSlug:       repoSlug,
					RequestedRef:   strings.TrimSpace(requestedRef),
					WorktreeSubdir: strings.TrimSpace(worktreeSubdir),
					CreationCommit: rs.cwdCreationCommitForRepo(cmd.Context(), selectedRepo),
				}
			}

			iss, err := rs.store.CreateIssue(cmd.Context(), store.CreateIssueInput{
				Prefix:      rs.prefix,
				Title:       title,
				Description: description,
				Priority:    priority,
				IssueType:   issueType,
				Labels:      labels,
				Actor:       rs.actor,
				Repo:        repoInput,
			})
			if err != nil {
				return fmt.Errorf("create: %w", err)
			}

			// --silent: bare id + newline on stdout, nothing else (bd compat).
			// This is load-bearing: skills capture ID=$(bn create ... --silent).
			if silent {
				fmt.Fprintln(cmd.OutOrStdout(), iss.ID)
				return nil
			}

			if rs.jsonOut {
				return writeJSONTo(cmd.OutOrStdout(), toIssueJSON(iss))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created %s: %s\n", iss.ID, iss.Title)
			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "issue description")
	cmd.Flags().IntVarP(&priority, "priority", "p", 2, "priority 0=critical, 1=high, 2=medium, 3=low, 4=backlog")
	cmd.Flags().StringArrayVarP(&labels, "label", "l", nil, "labels (repeatable: -l impl -l prep)")
	cmd.Flags().StringVarP(&issueType, "type", "t", "task", "issue type: bug|feature|task|epic|chore")
	cmd.Flags().StringVar(&repoSlug, "repo", "", "repo slug for workspace routing (defaults to active .bn repo)")
	cmd.Flags().StringVar(&requestedRef, "ref", "", "requested git ref for this issue")
	cmd.Flags().StringVar(&worktreeSubdir, "subdir", "", "worktree subdirectory override for this issue")
	cmd.Flags().BoolVar(&silent, "silent", false, "print only the id on stdout (for scripting)")
	return cmd
}
