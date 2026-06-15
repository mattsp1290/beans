package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

func newExportCmd(rs *appState) *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export issues as bd-compatible JSONL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			issues, err := rs.store.ListIssues(cmd.Context(), store.ListFilter{Prefix: rs.prefix})
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}

			// Fetch every edge kind (blocks + parent-child + any custom) so the
			// export round-trips hierarchy, not just blocking edges. ListDeps is
			// ordered (issue_id, blocked_by_id, dep_type) for stable output.
			edges, err := rs.store.ListDeps(cmd.Context(), rs.prefix)
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}
			depsByChild := make(map[string][]store.DepEdge, len(edges))
			for _, e := range edges {
				depsByChild[e.IssueID] = append(depsByChild[e.IssueID], e)
			}

			w := cmd.OutOrStdout()
			if outputPath != "" {
				f, err := os.Create(outputPath)
				if err != nil {
					return fmt.Errorf("export: create %s: %w", outputPath, err)
				}
				defer f.Close()
				w = f
			}

			if err := writeExportJSONL(w, issues, depsByChild); err != nil {
				return fmt.Errorf("export: write: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write JSONL export to file (default: stdout)")
	return cmd
}

func writeExportJSONL(w io.Writer, issues []store.Issue, depsByChild map[string][]store.DepEdge) error {
	enc := json.NewEncoder(w)
	for _, iss := range issues {
		if err := enc.Encode(toBDExportLine(iss, depsByChild[iss.ID])); err != nil {
			return err
		}
	}
	return nil
}

func toBDExportLine(iss store.Issue, edges []store.DepEdge) bdExportLine {
	labels := iss.Labels
	if labels == nil {
		labels = []string{}
	}

	// Emit every edge kind (blocks + parent-child + custom). edges are already
	// ordered by (blocked_by_id, dep_type) from ListDeps for stable output.
	deps := make([]bdExportDep, 0, len(edges))
	for _, e := range edges {
		depType := e.DepType
		if depType == "" {
			// Persisted rows are NOT NULL, so this only guards hand-built DepEdges
			// (e.g. in tests) that omit DepType; default them to blocks.
			depType = store.DepTypeBlocks
		}
		deps = append(deps, bdExportDep{
			IssueID:   e.IssueID,
			DependsOn: e.BlockedByID,
			Type:      depType,
		})
	}

	return bdExportLine{
		ID:           iss.ID,
		Title:        iss.Title,
		Description:  iss.Description,
		Status:       string(iss.State),
		Priority:     exportPriority(iss.Priority),
		IssueType:    iss.IssueType,
		Labels:       labels,
		BranchName:   iss.BranchName,
		URL:          iss.URL,
		Dependencies: deps,
	}
}

func exportPriority(p model.Priority) int {
	// model.Priority is 1-indexed; bd export uses 0-indexed priorities.
	prio := int(p) - 1
	if prio < 0 {
		return 0
	}
	return prio
}
