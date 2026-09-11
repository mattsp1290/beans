package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
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
	put := &cobra.Command{Use: "put <directory>", Short: "Publish a validated plan bundle", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(true, false); err != nil {
			return err
		}
		b, err := plan.Load(args[0])
		if err != nil {
			return err
		}
		op, res, err := ops.PlanPut(rs.opsEnv(), ops.PlanPutInput{Snapshot: b.Snapshot(), Prefix: rs.prefixFor(rs.resolved.Project)})
		if err != nil {
			return err
		}
		result, err := rs.mutate(cmd.Context(), op)
		if err != nil {
			return err
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"id": res.ID, "path": res.Path, "status": res.Status, "commit": result.SHA, "pushed": result.Pushed})
		}
		return rs.printCommit(cmd, result, "published "+res.ID)
	}}
	var status string
	list := &cobra.Command{Use: "list", Short: "List plans in the project", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(false, false); err != nil {
			return err
		}
		ix, err := rs.readIndex(cmd.Context())
		if err != nil {
			return err
		}
		ix.RLock()
		defer ix.RUnlock()
		out := []map[string]any{}
		for _, p := range ix.ProjectPlans(rs.resolved.Project) {
			if status != "" && string(p.Status) != status {
				continue
			}
			out = append(out, map[string]any{"id": p.ID, "status": p.Status, "updated": p.Updated, "title": p.Title})
		}
		if rs.jsonOut {
			return writeJSON(out)
		}
		w := tableWriter(cmd)
		for _, p := range out {
			fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", p["id"], p["status"], p["updated"], p["title"])
		}
		return w.Flush()
	}}
	list.Flags().StringVar(&status, "status", "", "filter exact lifecycle status")
	show := &cobra.Command{Use: "show <id>", Short: "Show a published plan", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(false, false); err != nil {
			return err
		}
		ix, err := rs.readIndex(cmd.Context())
		if err != nil {
			return err
		}
		ix.RLock()
		defer ix.RUnlock()
		p, ok := ix.PlanByID(args[0])
		if !ok {
			return notFound("plan %s not found", args[0])
		}
		if rs.jsonOut {
			return writeJSON(p)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n\n%s\n", p.Title, p.Status, p.Body)
		return nil
	}}
	root.AddCommand(init, validate, put, list, show)
	return root
}
