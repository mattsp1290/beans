package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/plan"
)

func newPlanCmd(rs *appState) *cobra.Command {
	root := &cobra.Command{Use: "plan", Short: "Create, validate, publish, and inspect project plans"}
	var output string
	init := &cobra.Command{
		Use: "init <title>", Short: "Create a local plan draft", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			if err := rs.setupProject(false, false); err != nil {
				return err
			}
			if _, err := os.Lstat(output); err == nil {
				return fmt.Errorf("plan output already exists: %s", output)
			} else if !os.IsNotExist(err) {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			ix.RLock()
			exists := func(id string) bool { _, ok := ix.Lookup(id); return ok }
			id := plan.NewID(rs.prefixFor(rs.resolved.Project), exists, ix.HubConfig.IDs.Length)
			ix.RUnlock()
			if err := plan.WriteScaffold(filepath.Clean(output), id, args[0], rs.clock()); err != nil {
				return err
			}
			if rs.jsonOut {
				return writeJSON(map[string]any{"directory": output, "id": id, "title": args[0], "status": plan.StatusDraft})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s (%s)\nnext: bn plan validate %s\n", output, id, output)
			return nil
		},
	}
	init.Flags().StringVar(&output, "output", "", "local directory for the draft")
	validate := &cobra.Command{Use: "validate <directory>", Short: "Validate a local plan bundle", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		b, err := plan.Load(args[0])
		if err != nil {
			if rs.jsonOut {
				_ = writeJSON(map[string]any{"valid": false, "issues": []map[string]string{{"code": "invalid", "message": err.Error()}}})
			}
			return err
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"valid": true, "id": b.Plan.ID, "status": b.Plan.Status, "issues": []any{}})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "valid %s (%s)\n", b.Plan.ID, b.Plan.Status)
		return nil
	}}
	root.AddCommand(init, validate)
	return root
}
