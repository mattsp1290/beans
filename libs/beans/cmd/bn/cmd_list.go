package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
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
				return rs.printChildren(cmd, f, epic, "list")
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
	cmd.Flags().StringVar(&status, "status", "", fmt.Sprintf("filter by state (known: %s)", strings.Join(rs.workflowConfig().StatusNames(), ", ")))
	cmd.Flags().BoolVar(&all, "all", false, "return all results (overrides default page cap)")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "list issues across all repositories (distinct from --all which controls page cap)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, fmt.Sprintf("max results (default %d; 0 = default cap)", defaultListLimit))
	return cmd
}

func newChildrenCmd(rs *appState) *cobra.Command {
	var allRepos bool

	cmd := &cobra.Command{
		Use:   "children <parent>",
		Short: "List non-blocking parent-child members of an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := rs.resolveListFilter(cmd.Context(), allRepos)
			if err != nil {
				return err
			}
			parentID := strings.TrimSpace(args[0])
			if parentID == "" {
				return fmt.Errorf("parent id must not be empty")
			}
			return rs.printChildren(cmd, f, parentID, "children")
		},
	}

	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "list children across all repositories")
	return cmd
}

func (rs *appState) printChildren(cmd *cobra.Command, f store.ListFilter, parentID, commandName string) error {
	members, err := rs.store.ListMembers(cmd.Context(), f, parentID)
	if err != nil {
		return fmt.Errorf("%s: %w", commandName, err)
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
