package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
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
		refreshed := false
		if current, loadErr := plan.Load(args[0]); loadErr == nil && samePlanSnapshot(current.Snapshot(), b.Snapshot()) {
			current.Plan.Updated = res.Updated
			if data, encodeErr := plan.Encode(current.Plan); encodeErr == nil {
				if writeErr := gitops.WritePlanFile(filepath.Join(args[0], "plan.md"), data); writeErr == nil {
					refreshed = true
				} else {
					fmt.Fprintf(rs.stderr, "bn: warning: published %s but could not refresh local revision: %v\n", res.ID, writeErr)
				}
			}
		}
		if !refreshed {
			fmt.Fprintf(rs.stderr, "bn: warning: published %s but local source changed; retrieve or merge before the next plan put\n", res.ID)
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"id": res.ID, "path": res.Path, "status": res.Status, "updated": res.Updated, "local_revision_refreshed": refreshed, "commit": result.SHA, "pushed": result.Pushed})
		}
		return rs.printCommit(cmd, result, "published "+res.ID)
	}}
	var getOutput string
	get := &cobra.Command{Use: "get <id>", Short: "Copy a published plan into a new local directory", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(getOutput) == "" {
			return fmt.Errorf("--output is required")
		}
		if err := rs.setupProject(false, false); err != nil {
			return err
		}
		if _, err := os.Lstat(getOutput); err == nil {
			return fmt.Errorf("plan output already exists: %s", getOutput)
		} else if !os.IsNotExist(err) {
			return err
		}
		ix, err := rs.readIndex(cmd.Context())
		if err != nil {
			return err
		}
		ix.RLock()
		p, ok := ix.PlanByID(args[0])
		ix.RUnlock()
		if !ok {
			return notFound("plan %s not found", args[0])
		}
		root := filepath.Join(rs.paths.Hub, filepath.FromSlash(strings.TrimSuffix(p.Path, "/plan.md")))
		bundle, err := plan.Load(root)
		if err != nil {
			return err
		}
		parent, base := filepath.Dir(getOutput), filepath.Base(getOutput)
		staged, err := os.MkdirTemp(parent, "."+base+".plan-get-")
		if err != nil {
			return err
		}
		published := false
		defer func() {
			if !published {
				_ = os.RemoveAll(staged)
			}
		}()
		for name, data := range bundle.Snapshot().Files {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(staged, filepath.FromSlash(name))), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(staged, filepath.FromSlash(name)), data, 0o644); err != nil {
				return err
			}
		}
		if _, err := plan.Load(staged); err != nil {
			return fmt.Errorf("validate staged plan: %w", err)
		}
		if _, err := os.Lstat(getOutput); err == nil {
			return fmt.Errorf("plan output already exists: %s", getOutput)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(staged, getOutput); err != nil {
			return err
		}
		published = true
		if rs.jsonOut {
			return writeJSON(map[string]any{"id": p.ID, "status": p.Status, "source": p.Path, "destination": getOutput})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "retrieved %s to %s\n", p.ID, getOutput)
		return nil
	}}
	get.Flags().StringVar(&getOutput, "output", "", "new local directory")
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
	var forceLink bool
	link := &cobra.Command{Use: "link <plan-id> <node-id> <issue-id>", Short: "Bind a plan graph node to an issue", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(true, false); err != nil {
			return err
		}
		op, res := ops.PlanLink(rs.opsEnv(), args[0], args[1], args[2], forceLink)
		result, err := rs.mutate(cmd.Context(), op)
		if err != nil {
			return err
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"plan_id": res.PlanID, "node_id": res.NodeID, "issue_id": res.IssueID, "ref": res.Ref, "updated": res.Updated, "commit": result.SHA, "pushed": result.Pushed, "message": result.Message})
		}
		return rs.printCommit(cmd, result, fmt.Sprintf("linked %s/%s to %s", res.PlanID, res.NodeID, res.IssueID))
	}}
	link.Flags().BoolVar(&forceLink, "force", false, "replace an existing ref")
	unlink := &cobra.Command{Use: "unlink <plan-id> <node-id> <issue-id>", Short: "Remove an expected plan issue binding", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(true, false); err != nil {
			return err
		}
		op, res := ops.PlanUnlink(rs.opsEnv(), args[0], args[1], args[2])
		result, err := rs.mutate(cmd.Context(), op)
		if err != nil {
			return err
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"plan_id": res.PlanID, "node_id": res.NodeID, "issue_id": res.IssueID, "ref": res.Ref, "updated": res.Updated, "commit": result.SHA, "pushed": result.Pushed, "message": result.Message})
		}
		return rs.printCommit(cmd, result, fmt.Sprintf("unlinked %s/%s", res.PlanID, res.NodeID))
	}}
	statusCmd := &cobra.Command{Use: "status <plan-id>", Short: "Show derived execution state for a plan", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(false, false); err != nil {
			return err
		}
		ix, err := rs.readIndex(cmd.Context())
		if err != nil {
			return err
		}
		ix.RLock()
		report, ok := ix.PlanExecution(args[0])
		ix.RUnlock()
		if !ok {
			return notFound("plan %s not found", args[0])
		}
		if report.Project != rs.resolved.Project {
			return fmt.Errorf("plan %s belongs to project %s, not %s", args[0], report.Project, rs.resolved.Project)
		}
		if rs.jsonOut {
			return writeJSON(report)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s — %s\nlifecycle: %s\nexecution: %s\n", report.PlanID, report.Title, report.LifecycleStatus, report.ExecutionState)
		if report.LifecycleMismatch {
			fmt.Fprintln(cmd.OutOrStdout(), "warning: execution and lifecycle differ; change lifecycle explicitly")
		}
		c := report.Counts
		fmt.Fprintf(cmd.OutOrStdout(), "counts: runnable=%d in_progress=%d held=%d blocked=%d done=%d missing=%d issue=%d missing_issue=%d unlinked=%d reference=%d distinct_issues=%d\n", c.Runnable, c.InProgress, c.Held, c.Blocked, c.Done, c.Missing, c.Issue, c.MissingIssue, c.Unlinked, c.Reference, c.DistinctIssues)
		for _, n := range report.Nodes {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s ref=%q", n.NodeID, n.Binding, n.Ref)
			if n.WorkState != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (%s)", n.WorkState)
			}
			if n.Issue != nil {
				fmt.Fprintf(cmd.OutOrStdout(), " %s [%s] %s — %s", n.Issue.ID, n.Issue.Status, n.Issue.Project, n.Issue.Title)
			}
			if n.HoldReason != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " reason=%s", n.HoldReason)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			for _, blocker := range n.Blockers {
				if blocker.Missing {
					fmt.Fprintf(cmd.OutOrStdout(), "  blocked by missing %s\n", blocker.Target)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  blocked by %s [%s] %s — %s\n", blocker.ID, blocker.Status, blocker.Project, blocker.Title)
				}
			}
		}
		return nil
	}}
	root.AddCommand(init, validate, put, get, list, show, link, unlink, statusCmd)
	return root
}

func samePlanSnapshot(a, b plan.BundleSnapshot) bool {
	if len(a.Files) != len(b.Files) {
		return false
	}
	for path, data := range a.Files {
		if string(data) != string(b.Files[path]) {
			return false
		}
	}
	return true
}
