package main

import (
	"fmt"
	"strings"

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

			workflow := rs.workflowConfig()
			issues, err := rs.store.ReadyIssues(cmd.Context(), f, workflow.Terminal, workflow.Active)
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
	statusWidth := issueStatusColumnWidth(issues)
	fmt.Fprintf(w, "%-26s  %-*s  %-3s  %s\n", "ID", statusWidth, "STATUS", "PRI", "TITLE")
	fmt.Fprintf(w, "%-26s  %-*s  %-3s  %s\n",
		"──────────────────────────", statusWidth, strings.Repeat("─", statusWidth), "───", "─────────────────────────────")
	for _, iss := range issues {
		fmt.Fprintf(w, "%-26s  %-*s  %-3s  %s\n",
			iss.ID, statusWidth, string(iss.State), priorityLabel(iss.Priority), iss.Title)
	}
}

func issueStatusColumnWidth(issues []store.Issue) int {
	width := len("ready_for_validation") + 2
	for _, iss := range issues {
		if n := len(string(iss.State)) + 2; n > width {
			width = n
		}
	}
	return width
}
