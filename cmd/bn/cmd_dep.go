package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

func newDepCmd(rs *appState) *cobra.Command {
	dep := &cobra.Command{Use: "dep", Short: "Manage dependencies between issues"}
	var kind string
	add := &cobra.Command{
		Use:   "add <child> <parent>",
		Short: "Make child blocked by parent (or set its parent with -t parent-child)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.DepAdd(rs.opsEnv(), args[0], args[1], kind))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, fmt.Sprintf("%s now depends on %s", args[0], args[1]))
		},
	}
	add.Flags().StringVarP(&kind, "type", "t", "blocks", "blocks or parent-child")
	var rkind string
	remove := &cobra.Command{
		Use:   "remove <child> <parent>",
		Short: "Remove a dependency",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			res, err := rs.mutate(cmd.Context(), ops.DepRemove(rs.opsEnv(), args[0], args[1], rkind))
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, fmt.Sprintf("%s no longer depends on %s", args[0], args[1]))
		},
	}
	remove.Flags().StringVarP(&rkind, "type", "t", "blocks", "blocks or parent-child")
	var all bool
	tree := &cobra.Command{
		Use:   "tree [id]",
		Short: "Show blockers as an indented tree",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			var roots []*issue.Issue
			if len(args) == 1 {
				iss, err := lookupIssue(ix, args[0])
				if err != nil {
					return err
				}
				roots = []*issue.Issue{iss}
			} else {
				roots = ix.ProjectIssues(rs.projectScope(all), false)
			}
			if rs.jsonOut {
				out := make([]map[string]any, 0, len(roots))
				for _, r := range roots {
					out = append(out, depTreeJSON(ix, r, map[string]bool{}))
				}
				return writeJSON(out)
			}
			for _, r := range roots {
				printDepTree(cmd, ix, r, 0, rs.projectScope(all), map[string]bool{})
			}
			return nil
		},
	}
	tree.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	cycles := &cobra.Command{
		Use:   "cycles",
		Short: "Report dependency cycles (exit 1 when any)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			cycles := ix.Cycles()
			if rs.jsonOut {
				if cycles == nil {
					cycles = [][]string{}
				}
				if err := writeJSON(cycles); err != nil {
					return err
				}
			} else if len(cycles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no cycles")
			} else {
				for _, c := range cycles {
					fmt.Fprintln(cmd.OutOrStdout(), strings.Join(c, " → "))
				}
			}
			if len(cycles) > 0 {
				return &codedError{code: exitUsage, msg: fmt.Sprintf("%d dependency cycle(s)", len(cycles))}
			}
			return nil
		},
	}
	dep.AddCommand(add, remove, tree, cycles)
	return dep
}

func printDepTree(cmd *cobra.Command, ix *vault.Index, iss *issue.Issue, depth int, project string, seen map[string]bool) {
	label := iss.ID
	if project != "" && iss.Project != project {
		label = iss.Project + ":" + iss.ID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s%s [%s] %s\n", strings.Repeat("  ", depth), label, iss.Status, iss.Title)
	if seen[iss.ID] {
		return
	}
	seen[iss.ID] = true
	resolved, unresolved := ix.Blockers(iss)
	for _, b := range resolved {
		printDepTree(cmd, ix, b, depth+1, project, seen)
	}
	for _, u := range unresolved {
		fmt.Fprintf(cmd.OutOrStdout(), "%s%s (missing)\n", strings.Repeat("  ", depth+1), u)
	}
}

func depTreeJSON(ix *vault.Index, iss *issue.Issue, seen map[string]bool) map[string]any {
	node := map[string]any{"id": iss.ID, "status": iss.Status, "title": iss.Title, "project": iss.Project}
	if seen[iss.ID] {
		return node
	}
	seen[iss.ID] = true
	resolved, unresolved := ix.Blockers(iss)
	blockers := make([]map[string]any, 0, len(resolved)+len(unresolved))
	for _, b := range resolved {
		blockers = append(blockers, depTreeJSON(ix, b, seen))
	}
	for _, u := range unresolved {
		blockers = append(blockers, map[string]any{"id": u, "missing": true})
	}
	node["blockers"] = blockers
	return node
}

func newChildrenCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "children <id>",
		Short: "List the issues whose parent is <id>",
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
			children := ix.Children(iss.ID)
			if rs.jsonOut {
				return writeIssuesJSON(ix, children)
			}
			printIssueTable(cmd, ix, children, true)
			return nil
		},
	}
}
