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

			w := cmd.OutOrStdout()
			if outputPath != "" {
				f, err := os.Create(outputPath)
				if err != nil {
					return fmt.Errorf("export: create %s: %w", outputPath, err)
				}
				defer f.Close()
				w = f
			}

			if err := writeExportJSONL(w, issues); err != nil {
				return fmt.Errorf("export: write: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write JSONL export to file (default: stdout)")
	return cmd
}

func writeExportJSONL(w io.Writer, issues []store.Issue) error {
	enc := json.NewEncoder(w)
	for _, iss := range issues {
		if err := enc.Encode(toBDExportLine(iss)); err != nil {
			return err
		}
	}
	return nil
}

func toBDExportLine(iss store.Issue) bdExportLine {
	labels := iss.Labels
	if labels == nil {
		labels = []string{}
	}

	deps := make([]bdExportDep, 0, len(iss.BlockedBy))
	for _, blockerID := range iss.BlockedBy {
		deps = append(deps, bdExportDep{
			IssueID:   iss.ID,
			DependsOn: blockerID,
			Type:      "blocks",
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
