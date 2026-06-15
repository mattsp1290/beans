package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

const defaultListLimit = 50

func newListCmd(rs *appState) *cobra.Command {
	var (
		status string
		all    bool
		limit  int
		epic   string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			// --epic lists the parent-child members (children) of an epic, the
			// authoritative way to assert "every epic has >=2 children". Unlike
			// the default list, members are returned in ALL states (open through
			// closed) so the count is stable as children are completed; --status
			// is not applied here.
			if epic != "" {
				members, err := rs.store.ListMembers(cmd.Context(), rs.prefix, epic)
				if err != nil {
					return fmt.Errorf("list: %w", err)
				}
				if rs.jsonOut {
					out := make([]issueJSON, len(members))
					for i, iss := range members {
						out[i] = toIssueJSON(iss)
					}
					return writeJSON(out)
				}
				printIssueTable(cmd, members)
				return nil
			}

			f := store.ListFilter{Prefix: rs.prefix}

			if status != "" {
				if !allowedStates[status] {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: unknown status %q (known: open, in_progress, blocked, closed)\n", status)
				}
				f.States = []model.IssueState{model.IssueState(status)}
			}

			if !all {
				if limit > 0 {
					f.Limit = limit
				} else {
					f.Limit = defaultListLimit
				}
			}

			issues, err := rs.store.ListIssues(cmd.Context(), f)
			if err != nil {
				return fmt.Errorf("list: %w", err)
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

	cmd.Flags().StringVar(&epic, "epic", "", "list parent-child members (children) of the given epic/parent id (all states)")
	cmd.Flags().StringVar(&status, "status", "", "filter by state (open, in_progress, closed, …)")
	cmd.Flags().BoolVar(&all, "all", false, "return all results (overrides default page cap)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, fmt.Sprintf("max results (default %d; 0 = default cap)", defaultListLimit))
	return cmd
}
