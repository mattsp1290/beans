package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

// lookupIssue finds an issue by id (or by link target) in the index.
func lookupIssue(ix *vault.Index, id string) (*issue.Issue, error) {
	if iss, ok := ix.Issues[id]; ok {
		return iss, nil
	}
	if n, ok := ix.Lookup(id); ok && n.Issue != nil {
		return n.Issue, nil
	}
	return nil, fmt.Errorf("%w: %s", ops.ErrNotFound, id)
}

func newShowCmd(rs *appState) *cobra.Command {
	var raw bool
	var includeArchivedHandoffs bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show an issue with its blockers, children, backlinks, and log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			iss, err := lookupIssue(ix, args[0])
			if err != nil {
				return err
			}
			if raw {
				data, err := os.ReadFile(filepath.Join(rs.paths.Hub, filepath.FromSlash(iss.Path)))
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			detail := toIssueDetailJSON(ix, iss)
			if !includeArchivedHandoffs {
				detail.Backlinks = filteredIssueBacklinks(ix, iss, false)
			}
			if rs.jsonOut {
				return writeJSON(detail)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s  %s\n", iss.ID, iss.Title)
			fmt.Fprintf(w, "status: %s  priority: P%d  type: %s  project: %s", iss.Status, iss.Priority, iss.Type, iss.Project)
			if iss.Archived {
				fmt.Fprint(w, "  (archived)")
			}
			fmt.Fprintln(w)
			if len(iss.Labels) > 0 {
				fmt.Fprintf(w, "labels: %s\n", strings.Join(iss.Labels, ", "))
			}
			if iss.Assignee != "" {
				fmt.Fprintf(w, "assignee: %s\n", iss.Assignee)
			}
			if detail.Parent != "" {
				fmt.Fprintf(w, "parent: %s\n", detail.Parent)
			}
			if iss.URL != "" {
				fmt.Fprintf(w, "url: %s\n", iss.URL)
			}
			fmt.Fprintf(w, "path: %s\n", iss.Path)
			if len(detail.Blockers) > 0 {
				fmt.Fprintln(w, "\nblocked by:")
				for _, b := range detail.Blockers {
					if b.Missing {
						fmt.Fprintf(w, "  %s (missing)\n", b.ID)
					} else {
						fmt.Fprintf(w, "  %s [%s] %s: %s\n", b.ID, b.Status, b.Project, b.Title)
					}
				}
			}
			if len(detail.Children) > 0 {
				fmt.Fprintln(w, "\nchildren:")
				for _, c := range detail.Children {
					if ci, ok := ix.Issues[c]; ok {
						fmt.Fprintf(w, "  %s [%s] %s\n", c, ci.Status, ci.Title)
					}
				}
			}
			if len(detail.Backlinks) > 0 {
				fmt.Fprintln(w, "\nbacklinks:")
				for _, b := range detail.Backlinks {
					fmt.Fprintf(w, "  %s (%s)\n", b.From, b.Kind)
				}
			}
			if strings.TrimSpace(iss.Description) != "" {
				fmt.Fprintf(w, "\n%s\n", strings.TrimRight(iss.Description, "\n"))
			}
			if len(iss.Log) > 0 {
				fmt.Fprintln(w, "\nlog:")
				for _, e := range iss.Log {
					if e.Raw != "" {
						fmt.Fprintf(w, "  %s\n", strings.TrimPrefix(e.Raw, "- "))
						continue
					}
					fmt.Fprintf(w, "  %s %s: %s\n", e.At.Format("2006-01-02 15:04"), e.Actor, strings.ReplaceAll(e.Event, "\n", "\n    "))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print the issue file as is")
	cmd.Flags().BoolVar(&includeArchivedHandoffs, "include-archived-handoffs", false, "include historical handoff backlinks")
	return cmd
}

func filteredIssueBacklinks(ix *vault.Index, iss *issue.Issue, include bool) []backlinkJSON {
	refs := ix.IssueBacklinks(noteBasename(iss), include)
	out := []backlinkJSON{}
	for _, r := range refs {
		b := backlinkJSON{From: r.From, Kind: string(r.Kind)}
		if n, ok := ix.Notes[r.From]; ok {
			b.Path = n.Path
			if n.Issue != nil {
				b.From = n.Issue.ID
			}
			if n.Handoff != nil {
				b.From = n.Handoff.ID
			}
		}
		out = append(out, b)
	}
	return out
}
