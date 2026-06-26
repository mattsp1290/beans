package main

import (
	"fmt"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newReadyCmd(rs *appState) *cobra.Command {
	var (
		limit    int
		allRepos bool
	)

	cmd := &cobra.Command{
		Use:   "ready",
		Short: "List open, unblocked issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := rs.resolveListFilter(cmd.Context(), allRepos)
			if err != nil {
				return err
			}

			issues, err := rs.store.ReadyIssues(cmd.Context(), f, activeWorkflow.Terminal, activeWorkflow.Active)
			if err != nil {
				return fmt.Errorf("ready: %w", err)
			}

			if limit > 0 && len(issues) > limit {
				issues = issues[:limit]
			}

			if rs.jsonOut {
				out := make([]issueJSON, len(issues))
				for i, iss := range issues {
					out[i] = toIssueJSON(iss)
				}
				return writeJSONTo(cmd.OutOrStdout(), out)
			}

			printIssueTable(cmd, issues)
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max results (0 = no limit)")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "list ready issues across all repositories")
	return cmd
}

func printIssueTable(cmd *cobra.Command, issues []store.Issue) {
	if len(issues) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No issues.")
		return
	}
	w := cmd.OutOrStdout()
	// STATUS is sized for the longest default status ("ready_for_validation",
	// 20 chars) plus padding so configured hold states stay column-aligned.
	fmt.Fprintf(w, "%-26s  %-22s  %-3s  %s\n", "ID", "STATUS", "PRI", "TITLE")
	fmt.Fprintf(w, "%-26s  %-22s  %-3s  %s\n",
		"──────────────────────────", "──────────────────────", "───", "─────────────────────────────")
	for _, iss := range issues {
		fmt.Fprintf(w, "%-26s  %-22s  %-3s  %s\n",
			iss.ID, string(iss.State), priorityLabel(iss.Priority), iss.Title)
	}
}
