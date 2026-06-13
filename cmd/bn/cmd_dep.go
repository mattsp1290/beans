package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newDepCmd(rs *appState) *cobra.Command {
	dep := &cobra.Command{
		Use:   "dep",
		Short: "Manage issue dependencies",
	}

	dep.AddCommand(
		newDepAddCmd(rs),
		newDepRemoveCmd(rs),
		newDepTreeCmd(rs),
		newDepCyclesCmd(rs),
	)
	return dep
}

func newDepAddCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "add <child> <parent>",
		Short: "Block child until parent is terminal",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			childID, parentID := args[0], args[1]
			err := rs.store.AddDep(cmd.Context(), childID, parentID)
			if err != nil {
				switch {
				case errors.Is(err, store.ErrNotFound):
					return fmt.Errorf("not found: %s or %s", childID, parentID)
				case errors.Is(err, store.ErrCycle):
					return fmt.Errorf("cycle: adding %s → %s would create a cycle", childID, parentID)
				case errors.Is(err, store.ErrDuplicateDep):
					fmt.Fprintf(cmd.OutOrStdout(), "%s already depends on %s\n", childID, parentID)
					return nil
				}
				return fmt.Errorf("dep add: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s now depends on %s\n", childID, parentID)
			return nil
		},
	}
}

func newDepRemoveCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <child> <parent>",
		Short: "Remove a dependency edge",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}
			childID, parentID := args[0], args[1]
			err := rs.store.RemoveDep(cmd.Context(), childID, parentID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("dep not found: %s → %s", childID, parentID)
				}
				return fmt.Errorf("dep remove: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s → %s\n", childID, parentID)
			return nil
		},
	}
}

func newDepTreeCmd(rs *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Print the dependency tree",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			edges, err := rs.store.ListDeps(cmd.Context(), rs.prefix)
			if err != nil {
				return fmt.Errorf("dep tree: %w", err)
			}

			if rs.jsonOut {
				type edgeJSON struct {
					Issue     string `json:"issue"`
					BlockedBy string `json:"blocked_by"`
				}
				out := make([]edgeJSON, len(edges))
				for i, e := range edges {
					out[i] = edgeJSON{Issue: e.IssueID, BlockedBy: e.BlockedByID}
				}
				return writeJSON(out)
			}

			if len(edges) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No dependencies.")
				return nil
			}

			// Build adjacency: parent → children (who are blocked by parent).
			children := make(map[string][]string)
			isChild := make(map[string]bool)
			for _, e := range edges {
				children[e.BlockedByID] = append(children[e.BlockedByID], e.IssueID)
				isChild[e.IssueID] = true
			}

			// Collect unique root nodes (have children but are not a child themselves).
			seen := make(map[string]bool)
			var roots []string
			for _, e := range edges {
				if !isChild[e.BlockedByID] && !seen[e.BlockedByID] {
					roots = append(roots, e.BlockedByID)
					seen[e.BlockedByID] = true
				}
			}

			w := cmd.OutOrStdout()
			for i, root := range roots {
				printDepSubtree(w, root, children, "", i == len(roots)-1, map[string]bool{})
			}
			return nil
		},
	}
	return cmd
}

func printDepSubtree(w io.Writer, id string, children map[string][]string, prefix string, isLast bool, onPath map[string]bool) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	if onPath[id] {
		fmt.Fprintf(w, "%s%s%s (cycle)\n", prefix, connector, id)
		return
	}
	fmt.Fprintf(w, "%s%s%s\n", prefix, connector, id)

	onPath[id] = true
	defer delete(onPath, id)

	kids := children[id]
	childPrefix := prefix + "│   "
	if isLast {
		childPrefix = prefix + "    "
	}
	for i, kid := range kids {
		printDepSubtree(w, kid, children, childPrefix, i == len(kids)-1, onPath)
	}
}

func newDepCyclesCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "cycles",
		Short: "Detect cycles in the dependency graph",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			edges, err := rs.store.ListDeps(cmd.Context(), rs.prefix)
			if err != nil {
				return fmt.Errorf("dep cycles: %w", err)
			}

			cycles := detectCycles(edges)
			if len(cycles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No cycles detected.")
				return nil
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Cycles detected (%d):\n", len(cycles))
			for _, cycle := range cycles {
				fmt.Fprintf(w, "  %s\n", strings.Join(cycle, " → "))
			}
			return fmt.Errorf("%d cycle(s) detected", len(cycles))
		},
	}
}

// detectCycles performs a DFS to find any cycles in the dep graph.
func detectCycles(edges []store.DepEdge) [][]string {
	adj := make(map[string][]string)
	nodes := make(map[string]bool)
	for _, e := range edges {
		adj[e.IssueID] = append(adj[e.IssueID], e.BlockedByID)
		nodes[e.IssueID] = true
		nodes[e.BlockedByID] = true
	}

	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var cycles [][]string

	var dfs func(node string, path []string)
	dfs = func(node string, path []string) {
		if inStack[node] {
			for i, p := range path {
				if p == node {
					cycle := make([]string, len(path[i:]))
					copy(cycle, path[i:])
					cycles = append(cycles, append(cycle, node))
					return
				}
			}
			return
		}
		if visited[node] {
			return
		}
		visited[node] = true
		inStack[node] = true
		newPath := append(path, node) //nolint:gocritic
		for _, next := range adj[node] {
			dfs(next, newPath)
		}
		inStack[node] = false
	}

	for node := range nodes {
		if !visited[node] {
			dfs(node, nil)
		}
	}
	return cycles
}
