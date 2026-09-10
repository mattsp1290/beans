package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
)

func newImportCmd(rs *appState) *cobra.Command {
	imp := &cobra.Command{Use: "import", Short: "One-time imports into the hub"}
	var project string
	var dryRun, force bool
	bd := &cobra.Command{
		Use:   "bd <export.jsonl>",
		Short: "Import a bd export (bd export -o file) as one commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(project) == "" {
				return errors.New("--project <name> is required")
			}
			rs.project = project
			if err := rs.setupProject(true, false); err != nil {
				return err
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			recs, warnings, err := ops.ParseBDExport(f)
			if err != nil {
				return err
			}
			op, rep := ops.ImportBD(rs.opsEnv(), recs, dryRun, force)
			var sha string
			var pushed bool
			if dryRun {
				if _, err := op.Apply(rs.paths.Hub); err != nil {
					printImportReport(cmd, rep, warnings)
					return err
				}
			} else {
				res, err := rs.mutate(cmd.Context(), op)
				if err != nil {
					return err
				}
				sha, pushed = res.SHA, res.Pushed
			}
			rep.Warnings = append(rep.Warnings, warnings...)
			if rs.jsonOut {
				return writeJSON(map[string]any{"report": rep, "dry_run": dryRun, "commit": sha, "pushed": pushed})
			}
			printImportReport(cmd, rep, nil)
			switch {
			case dryRun:
				fmt.Fprintln(cmd.OutOrStdout(), "dry run: nothing written")
			case sha == "":
				fmt.Fprintf(cmd.OutOrStdout(), "import into project %s: no change\n", project)
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "imported into project %s (%s)\n", project, sha[:7])
			}
			return nil
		},
	}
	bd.Flags().StringVar(&project, "project", "", "target project (required)")
	bd.Flags().BoolVar(&dryRun, "dry-run", false, "report the mapping and write nothing")
	bd.Flags().BoolVar(&force, "force", false, "overwrite files that already exist")
	imp.AddCommand(bd)
	return imp
}

func printImportReport(cmd *cobra.Command, rep *ops.ImportReport, parseWarnings []string) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "issues: %d (%d archived)  memories: %d  blocks: %d  parents: %d\n", rep.Issues, rep.Archived, rep.Memories, rep.Blocks, rep.Parents)
	if len(rep.Actors) > 0 {
		names := make([]string, 0, len(rep.Actors))
		for k := range rep.Actors {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(w, "actor: %q → %s\n", n, rep.Actors[n])
		}
	}
	for _, s := range parseWarnings {
		fmt.Fprintf(w, "warning: %s\n", s)
	}
	for _, s := range rep.Warnings {
		fmt.Fprintf(w, "warning: %s\n", s)
	}
	for _, s := range rep.Unresolved {
		fmt.Fprintf(w, "unresolved: %s\n", s)
	}
	for _, s := range rep.Rejected {
		fmt.Fprintf(w, "rejected: %s\n", s)
	}
}
