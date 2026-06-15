package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

// defaultTerminalStates are the states treated as "done" for ready semantics.
// Operators who need custom terminal sets can extend via a future config flag.
var defaultTerminalStates = []model.IssueState{"closed", "done"}

// defaultActiveStates are the states eligible for work dispatch.
var defaultActiveStates = []model.IssueState{"open"}

func newReadyCmd(rs *appState) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "ready",
		Short: "List open, unblocked issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			issues, err := rs.store.ReadyIssues(
				cmd.Context(),
				store.ListFilter{Prefix: rs.prefix},
				defaultTerminalStates,
				defaultActiveStates,
			)
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
				return writeJSON(out)
			}

			printIssueTable(cmd, issues)
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max results (0 = no limit)")
	return cmd
}

func printIssueTable(cmd *cobra.Command, issues []store.Issue) {
	if len(issues) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No issues.")
		return
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-26s  %-12s  %-3s  %s\n", "ID", "STATUS", "PRI", "TITLE")
	fmt.Fprintf(w, "%-26s  %-12s  %-3s  %s\n",
		"──────────────────────────", "────────────", "───", "─────────────────────────────")
	for _, iss := range issues {
		fmt.Fprintf(w, "%-26s  %-12s  %-3s  %s\n",
			iss.ID, string(iss.State), priorityLabel(iss.Priority), iss.Title)
	}
}
