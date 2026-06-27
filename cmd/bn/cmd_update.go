package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

func newUpdateCmd(rs *appState) *cobra.Command {
	var (
		claim          bool
		status         string
		title          string
		description    string
		notes          string
		appendNotes    bool // accepted for bd compat; --notes always appends in bn
		force          bool
		repoSlug       string
		requestedRef   string
		worktreeSubdir string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update issue fields",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			id := args[0]
			in := store.UpdateIssueInput{}
			repoChanged := cmd.Flags().Changed("repo") || cmd.Flags().Changed("ref") || cmd.Flags().Changed("subdir")
			workflow := rs.workflowConfig()

			// D6: check if the target state change would re-open a terminal issue.
			// Both --claim (→ in_progress) and --status=<non-terminal> re-open.
			wantsReopen := claim || repoChanged || (status != "" && !workflow.IsTerminal(model.IssueState(status)))
			if wantsReopen {
				cur, err := rs.store.GetIssue(cmd.Context(), id)
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return fmt.Errorf("not found: %s", id)
					}
					return fmt.Errorf("update: %w", err)
				}
				if workflow.IsTerminal(cur.State) && !force {
					return fmt.Errorf(
						"issue %s is %s (terminal); use --force to re-open",
						id, cur.State,
					)
				}
				if repoChanged && cur.State == "in_progress" && !force {
					return fmt.Errorf(
						"issue %s is in_progress; use --force to change repo routing",
						id,
					)
				}
			}

			if claim {
				st := model.IssueState("in_progress")
				in.State = &st
				if notes == "" && rs.actor != "" {
					notes = fmt.Sprintf("claimed by %s", rs.actor)
				}
			}

			if status != "" {
				if !workflow.IsValid(model.IssueState(status)) {
					return fmt.Errorf("invalid status %q (allowed: %s)", status, strings.Join(workflow.StatusNames(), ", "))
				}
				st := model.IssueState(status)
				in.State = &st
			}

			if title != "" {
				in.Title = &title
			}

			if description != "" {
				in.Description = &description
			}

			if notes != "" {
				in.AppendNotes = &store.AppendNotesInput{
					Actor: rs.actor,
					Body:  notes,
				}
			}

			if repoChanged {
				repoSlug = strings.TrimSpace(repoSlug)
				if repoSlug == "" {
					cfg, err := readActiveProjectConfig("")
					if err != nil {
						return fmt.Errorf("update: read active repo marker: %w", err)
					}
					repoSlug = cfg.Repo
				}
				if repoSlug == "" {
					return fmt.Errorf("--repo is required when --ref or --subdir is set outside an activated repo")
				}
				cleaned, err := cleanRepoSlug(repoSlug)
				if err != nil {
					return err
				}
				in.Repo = &store.IssueRepoInput{
					RepoSlug:       cleaned,
					RequestedRef:   strings.TrimSpace(requestedRef),
					WorktreeSubdir: strings.TrimSpace(worktreeSubdir),
				}
			}

			_ = appendNotes // --notes always appends in bn; flag accepted for compat

			iss, err := rs.store.UpdateIssue(cmd.Context(), id, in)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("not found: %s", id)
				}
				return fmt.Errorf("update: %w", err)
			}

			if rs.jsonOut {
				return writeJSON(toIssueJSON(iss))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", iss.ID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&claim, "claim", false, "transition to in_progress and note 'claimed by <actor>'")
	cmd.Flags().StringVar(&status, "status", "", "set state (must be in configured workflow statuses)")
	cmd.Flags().StringVar(&title, "title", "", "set title")
	cmd.Flags().StringVar(&description, "description", "", "set description")
	cmd.Flags().StringVar(&notes, "notes", "", "append a note")
	cmd.Flags().BoolVar(&appendNotes, "append-notes", false, "notes are always appended (accepted for bd compat)")
	cmd.Flags().BoolVar(&force, "force", false, "allow re-opening a terminal issue")
	cmd.Flags().StringVar(&repoSlug, "repo", "", "repo slug for workspace routing")
	cmd.Flags().StringVar(&requestedRef, "ref", "", "requested git ref for this issue")
	cmd.Flags().StringVar(&worktreeSubdir, "subdir", "", "worktree subdirectory override for this issue")
	return cmd
}
