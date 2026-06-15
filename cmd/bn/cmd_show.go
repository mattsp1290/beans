package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newShowCmd(rs *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show issue details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			iss, err := rs.store.GetIssue(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					fmt.Fprintf(cmd.ErrOrStderr(), "not found: %s\n", args[0])
					return fmt.Errorf("not found: %s", args[0])
				}
				return fmt.Errorf("show: %w", err)
			}
			warnIfCrossRepo(cmd.ErrOrStderr(), rs, iss)

			if rs.jsonOut {
				return writeJSON(toIssueJSON(iss))
			}

			printIssueDetail(cmd, iss)

			// Surface parent-child membership (non-blocking, so absent from
			// BlockedBy): which epic(s) this issue belongs to, and — if it is a
			// rollup — its children.
			parents, err := rs.store.ListParents(cmd.Context(), rs.prefix, iss.ID)
			if err != nil {
				return fmt.Errorf("show: %w", err)
			}
			children, err := rs.store.ListMembers(cmd.Context(), rs.prefix, iss.ID)
			if err != nil {
				return fmt.Errorf("show: %w", err)
			}
			printMembership(cmd, parents, children)
			return nil
		},
	}
	return cmd
}

func printIssueDetail(cmd *cobra.Command, iss store.Issue) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  [%s]  %s\n", iss.ID, iss.State, priorityLabel(iss.Priority))
	fmt.Fprintln(w, strings.Repeat("─", 60))
	fmt.Fprintf(w, "Title:  %s\n", iss.Title)
	fmt.Fprintf(w, "Type:   %s\n", iss.IssueType)
	if len(iss.Labels) > 0 {
		fmt.Fprintf(w, "Labels: %s\n", strings.Join(iss.Labels, ", "))
	}
	if iss.Description != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Description:")
		for _, line := range strings.Split(iss.Description, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if len(iss.BlockedBy) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Blocked by:")
		for _, dep := range iss.BlockedBy {
			fmt.Fprintf(w, "  %s\n", dep)
		}
	}
	if iss.Repo != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Repo:   %s\n", iss.Repo.Slug)
		fmt.Fprintf(w, "Remote: %s\n", iss.Repo.RemoteURL)
		if iss.Repo.BaseRef != "" {
			fmt.Fprintf(w, "Base:   %s\n", iss.Repo.BaseRef)
		}
		if iss.Repo.RequestedRef != "" {
			fmt.Fprintf(w, "Ref:    %s\n", iss.Repo.RequestedRef)
		}
		if iss.Repo.WorktreeSubdir != "" {
			fmt.Fprintf(w, "Subdir: %s\n", iss.Repo.WorktreeSubdir)
		}
	}
	if iss.BranchName != "" {
		fmt.Fprintf(w, "\nBranch: %s\n", iss.BranchName)
	}
	if iss.URL != "" {
		fmt.Fprintf(w, "URL:    %s\n", iss.URL)
	}
	fmt.Fprintf(w, "\nCreated: %s\n", iss.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Updated: %s\n", iss.UpdatedAt.Format("2006-01-02 15:04:05"))
}

// printMembership renders the parent-child relationships for an issue: the
// epic(s) it belongs to (parents) and, when it is a rollup, its children.
func printMembership(cmd *cobra.Command, parents, children []store.Issue) {
	w := cmd.OutOrStdout()
	if len(parents) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Member of:")
		for _, p := range parents {
			fmt.Fprintf(w, "  %s  %s\n", p.ID, p.Title)
		}
	}
	if len(children) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Children:")
		for _, c := range children {
			fmt.Fprintf(w, "  %s  [%s]  %s\n", c.ID, c.State, c.Title)
		}
	}
}
