package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
)

const defaultListLimit = 50

func newListCmd(rs *appState) *cobra.Command {
	var (
		status   string
		all      bool
		allRepos bool
		limit    int
		epic     string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := rs.resolveListFilter(cmd.Context(), allRepos)
			if err != nil {
				return err
			}

			// --epic lists the parent-child members (children) of an epic, the
			// authoritative way to assert "every epic has >=2 children". Unlike
			// the default list, members are returned in ALL states (open through
			// closed) so the count is stable as children are completed; --status
			// is not applied here.
			if epic != "" {
				members, err := rs.store.ListMembers(cmd.Context(), f, epic)
				if err != nil {
					return fmt.Errorf("list: %w", err)
				}
				if rs.jsonOut {
					out := make([]issueJSON, len(members))
					for i, iss := range members {
						out[i] = toIssueJSON(iss)
					}
					return writeJSONTo(cmd.OutOrStdout(), out)
				}
				printIssueTable(cmd, members)
				return nil
			}

			if status != "" {
				workflow := rs.workflowConfig()
				if !workflow.IsValid(model.IssueState(status)) {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: unknown status %q (known: %s)\n", status, strings.Join(workflow.StatusNames(), ", "))
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
				return writeJSONTo(cmd.OutOrStdout(), out)
			}

			printIssueTable(cmd, issues)
			return nil
		},
	}

	cmd.Flags().StringVar(&epic, "epic", "", "list parent-child members (children) of the given epic/parent id (all states)")
	cmd.Flags().StringVar(&status, "status", "", "filter by configured workflow state")
	cmd.Flags().BoolVar(&all, "all", false, "return all results (overrides default page cap)")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "list issues across all repositories (distinct from --all which controls page cap)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, fmt.Sprintf("max results (default %d; 0 = default cap)", defaultListLimit))
	return cmd
}
