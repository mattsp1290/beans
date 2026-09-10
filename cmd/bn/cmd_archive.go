package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
)

// parseAge parses durations like 30d, 12h, 90m.
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func newArchiveCmd(rs *appState) *cobra.Command {
	var olderThan string
	var dryRun, all bool
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Move closed issues older than --older-than into archive/<year>/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			age, err := parseAge(olderThan)
			if err != nil {
				return err
			}
			if err := rs.setupProject(false, all); err != nil {
				return err
			}
			op, res := ops.Archive(rs.opsEnv(), rs.projectScope(all), age, dryRun)
			var sha string
			var pushed bool
			if dryRun {
				if _, err := op.Apply(rs.paths.Hub); err != nil {
					return err
				}
			} else {
				out, err := rs.mutate(cmd.Context(), op)
				if err != nil {
					return err
				}
				sha, pushed = out.SHA, out.Pushed
			}
			if rs.jsonOut {
				moved := res.Moved
				if moved == nil {
					moved = []string{}
				}
				return writeJSON(map[string]any{"moved": moved, "dry_run": dryRun, "commit": sha, "pushed": pushed})
			}
			if len(res.Moved) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to archive")
				return nil
			}
			verb := "archived"
			if dryRun {
				verb = "would archive"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d issue(s): %s\n", verb, len(res.Moved), strings.Join(res.Moved, ", "))
			return nil
		},
	}
	cmd.Flags().StringVar(&olderThan, "older-than", "30d", "age of the last log entry, e.g. 30d or 12h")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list without moving")
	cmd.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	return cmd
}
