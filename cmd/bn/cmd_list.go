package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

type listFilter struct {
	status, typ, label, assignee string
	archived, closed, all        bool
	limit                        int
	sortBy                       string
}

func (rs *appState) projectScope(all bool) string {
	if all {
		return ""
	}
	return rs.resolved.Project
}

func filterIssues(ix *vault.Index, project string, f listFilter) []*issue.Issue {
	var out []*issue.Issue
	for _, iss := range ix.Issues {
		if project != "" && iss.Project != project {
			continue
		}
		wf := ix.WorkflowFor(iss.Project)
		if f.closed {
			if !wf.IsTerminal(iss.Status) {
				continue
			}
		} else {
			if iss.Archived && !f.archived {
				continue
			}
			if f.status == "" && !f.archived && wf.IsTerminal(iss.Status) {
				continue
			}
		}
		if f.status != "" && iss.Status != f.status {
			continue
		}
		if f.typ != "" && iss.Type != f.typ {
			continue
		}
		if f.label != "" && !containsString(iss.Labels, f.label) {
			continue
		}
		if f.assignee != "" && iss.Assignee != f.assignee {
			continue
		}
		out = append(out, iss)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch f.sortBy {
		case "updated":
			if !a.Updated.Equal(b.Updated) {
				return a.Updated.After(b.Updated)
			}
		case "priority":
			if a.Priority != b.Priority {
				return a.Priority < b.Priority
			}
		default:
			if !a.Created.Equal(b.Created) {
				return a.Created.Before(b.Created)
			}
		}
		return a.ID < b.ID
	})
	if f.limit > 0 && len(out) > f.limit {
		out = out[:f.limit]
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// printIssueTable prints the shared id/status/pri/type/title table.
func printIssueTable(cmd *cobra.Command, ix *vault.Index, issues []*issue.Issue, showProject bool) {
	w := tableWriter(cmd)
	for _, iss := range issues {
		line := fmt.Sprintf("%s\t%s\tP%d\t%s\t%s", iss.ID, iss.Status, iss.Priority, iss.Type, iss.Title)
		if showProject {
			line += "\t" + iss.Project
		}
		fmt.Fprintln(w, line)
	}
	_ = w.Flush()
	_ = ix
}

func writeIssuesJSON(ix *vault.Index, issues []*issue.Issue) error {
	out := make([]issueJSON, 0, len(issues))
	for _, iss := range issues {
		out = append(out, toIssueJSON(ix, iss))
	}
	return writeJSON(out)
}

func newListCmd(rs *appState) *cobra.Command {
	var f listFilter
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues (open by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupProject(false, f.all); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			issues := filterIssues(ix, rs.projectScope(f.all), f)
			if rs.jsonOut {
				return writeIssuesJSON(ix, issues)
			}
			printIssueTable(cmd, ix, issues, f.all)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.status, "status", "", "only this status")
	fl.StringVar(&f.typ, "type", "", "only this type")
	fl.StringVar(&f.label, "label", "", "only issues with this label")
	fl.StringVar(&f.assignee, "assignee", "", "only this assignee")
	fl.BoolVar(&f.archived, "archived", false, "include archived issues")
	fl.BoolVar(&f.closed, "closed", false, "only terminal issues (implies --archived)")
	fl.BoolVar(&f.all, "all-projects", false, "every project in the hub")
	fl.IntVarP(&f.limit, "limit", "n", 50, "maximum rows (0 = all)")
	fl.StringVar(&f.sortBy, "sort", "created", "sort by created, updated, or priority")
	return cmd
}

func newReadyCmd(rs *appState) *cobra.Command {
	var all bool
	var limit int
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Issues with no open blockers, highest priority first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupProject(false, all); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			issues := ix.Ready(rs.projectScope(all), all)
			if limit > 0 && len(issues) > limit {
				issues = issues[:limit]
			}
			if rs.jsonOut {
				return writeIssuesJSON(ix, issues)
			}
			printIssueTable(cmd, ix, issues, all)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "maximum rows (0 = all)")
	return cmd
}

func newBlockedCmd(rs *appState) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "blocked",
		Short: "Issues waiting on at least one open blocker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupProject(false, all); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			blocked := ix.Blocked(rs.projectScope(all), all)
			if rs.jsonOut {
				type row struct {
					issueJSON
					Blockers []string `json:"blockers"`
				}
				out := make([]row, 0, len(blocked))
				for _, b := range blocked {
					out = append(out, row{toIssueJSON(ix, b.Issue), b.Blockers})
				}
				return writeJSON(out)
			}
			w := tableWriter(cmd)
			for _, b := range blocked {
				fmt.Fprintf(w, "%s\t%s\tP%d\t%s\tblocked by %s\n", b.Issue.ID, b.Issue.Status, b.Issue.Priority, b.Issue.Title, strings.Join(b.Blockers, ", "))
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	return cmd
}
