package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

func requestBody(cmd *cobra.Command, description, file string, stdin bool) (string, bool, error) {
	n := 0
	if cmd.Flags().Changed("description") {
		n++
	}
	if file != "" {
		n++
	}
	if stdin {
		n++
	}
	if n > 1 {
		return "", false, fmt.Errorf("only one of --description, --body-file, and --stdin may be used")
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", false, fmt.Errorf("read body file %s: %w", file, err)
		}
		return string(b), true, nil
	}
	if stdin {
		b, err := io.ReadAll(cmd.InOrStdin())
		return string(b), true, err
	}
	return description, n == 1, nil
}

func requestJSON(r *issue.Request) map[string]any {
	return map[string]any{"id": r.ID, "title": r.Title, "status": r.Status, "priority": r.Priority, "labels": r.Labels, "requested_by": r.RequestedBy, "issues": r.Issues, "created": r.Created, "updated": r.Updated, "project": r.Project, "path": r.Path, "body": r.Body, "log": r.Log}
}

func requestDetailJSON(ix *vault.Index, r *issue.Request) map[string]any {
	out := requestJSON(r)
	issues := make([]map[string]any, 0, len(r.Issues))
	for _, link := range r.Issues {
		if n, ok := ix.Lookup(link.Target); ok && n.Issue != nil {
			issues = append(issues, map[string]any{"id": n.Issue.ID, "title": n.Issue.Title, "status": n.Issue.Status, "priority": n.Issue.Priority, "project": n.Issue.Project, "archived": n.Issue.Archived})
		} else {
			issues = append(issues, map[string]any{"id": link.Target, "missing": true})
		}
	}
	out["linked_issues"] = issues
	return out
}

func newRequestCmd(rs *appState) *cobra.Command {
	root := &cobra.Command{Use: "request", Short: "Create and manage project requests"}
	var create ops.RequestCreateInput
	var createFile string
	var createStdin, silent bool
	newCreate := &cobra.Command{Use: "create <title>", Short: "Create a request", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(true, false); err != nil {
			return err
		}
		body, provided, err := requestBody(cmd, create.Body, createFile, createStdin)
		if err != nil {
			return err
		}
		create.Title = args[0]
		create.Body = body
		create.BodyProvided = provided
		if rs.resolved.Created {
			create.Remote = rs.resolved.RepoRemote
		}
		op, result := ops.RequestCreate(rs.opsEnv(), create, rs.prefixFor(rs.resolved.Project))
		out, err := rs.mutate(cmd.Context(), op)
		if err != nil {
			return err
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"id": result.ID, "path": result.Path, "commit": out.SHA, "pushed": out.Pushed, "message": out.Message})
		}
		if silent {
			fmt.Fprintln(cmd.OutOrStdout(), result.ID)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "created %s: %s\n", result.ID, create.Title)
		}
		return nil
	}}
	cf := newCreate.Flags()
	cf.StringVarP(&create.Body, "description", "d", "", "request Markdown")
	cf.StringVar(&createFile, "body-file", "", "read request Markdown from file")
	cf.BoolVar(&createStdin, "stdin", false, "read request Markdown from stdin")
	cf.IntVarP(&create.Priority, "priority", "p", 2, "priority 0 to 4")
	cf.StringArrayVarP(&create.Labels, "label", "l", nil, "label (repeatable)")
	cf.StringVar(&create.RequestedBy, "requested-by", "", "requester")
	cf.StringArrayVar(&create.Issues, "issue", nil, "linked issue id (repeatable)")
	cf.BoolVar(&silent, "silent", false, "print only the new id")

	var status, label, query string
	var priority int
	var terminal, all bool
	list := &cobra.Command{Use: "list", Short: "List requests", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(false, all); err != nil {
			return err
		}
		if status != "" && !issue.ValidRequestStatus(status) {
			return fmt.Errorf("invalid request status %q", status)
		}
		ix, err := rs.readIndex(cmd.Context())
		if err != nil {
			return err
		}
		var p *int
		if cmd.Flags().Changed("priority") {
			if priority < 0 || priority > 4 {
				return fmt.Errorf("priority must be 0-4")
			}
			p = &priority
		}
		reqs := ix.ProjectRequests(rs.projectScope(all), status, label, p, query, terminal)
		if rs.jsonOut {
			out := make([]map[string]any, 0, len(reqs))
			for _, r := range reqs {
				out = append(out, requestJSON(r))
			}
			return writeJSON(out)
		}
		w := tableWriter(cmd)
		for _, r := range reqs {
			fmt.Fprintf(w, "%s\t%s\tP%d\t%s\n", r.ID, r.Status, r.Priority, r.Title)
		}
		return w.Flush()
	}}
	lf := list.Flags()
	lf.StringVar(&status, "status", "", "only this status")
	lf.StringVar(&label, "label", "", "only this label")
	lf.StringVar(&query, "query", "", "text query")
	lf.IntVarP(&priority, "priority", "p", 0, "only this priority")
	lf.BoolVar(&terminal, "terminal", false, "include terminal requests")
	lf.BoolVar(&all, "all-projects", false, "every project")

	show := &cobra.Command{Use: "show <request-id>", Short: "Show a request", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(false, true); err != nil {
			return err
		}
		ix, err := rs.readIndex(cmd.Context())
		if err != nil {
			return err
		}
		r, ok := ix.RequestByID(args[0])
		if !ok {
			return fmt.Errorf("%w: %s", ops.ErrRequestNotFound, args[0])
		}
		if rs.jsonOut {
			return writeJSON(requestDetailJSON(ix, r))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\nstatus: %s  priority: P%d  project: %s\n\n%s", r.ID, r.Title, r.Status, r.Priority, r.Project, r.Body)
		if len(r.Issues) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "\nlinked issues:")
			for _, linked := range requestDetailJSON(ix, r)["linked_issues"].([]map[string]any) {
				if linked["missing"] == true {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s (missing)\n", linked["id"])
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s] %s\n", linked["id"], linked["status"], linked["title"])
			}
		}
		return nil
	}}

	var update ops.RequestUpdateInput
	var updateFile, updateStatus, updateTitle, updateRequestedBy, desc string
	var updateStdin bool
	updateCmd := &cobra.Command{Use: "update <request-id>", Short: "Update a request", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rs.setupProject(true, false); err != nil {
			return err
		}
		body, provided, err := requestBody(cmd, desc, updateFile, updateStdin)
		if err != nil {
			return err
		}
		if provided {
			update.Body = &body
		}
		if err := validateRequestUpdate(&update); err != nil {
			return err
		}
		out, err := rs.mutate(cmd.Context(), ops.RequestUpdate(rs.opsEnv(), args[0], update))
		if err != nil {
			return err
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"commit": out.SHA, "pushed": out.Pushed, "message": out.Message})
		}
		return rs.printCommit(cmd, out, "updated "+args[0])
	}}
	uf := updateCmd.Flags()
	uf.StringVar(&updateStatus, "status", "", "new status")
	uf.StringVar(&updateTitle, "title", "", "new title")
	uf.StringVar(&updateFile, "body-file", "", "read body from file")
	uf.BoolVar(&updateStdin, "stdin", false, "read body from stdin")
	uf.StringVarP(&desc, "description", "d", "", "new request body")
	uf.Int("priority", 0, "new priority")
	uf.StringVar(&updateRequestedBy, "requested-by", "", "new requester")
	uf.StringArrayVar(&update.AddLabels, "label", nil, "add label")
	uf.StringArrayVar(&update.RemoveLabels, "unlabel", nil, "remove label")
	uf.BoolVar(&update.Force, "force", false, "allow any valid status correction")
	// Pointer flags need to distinguish absent from their zero value.
	updateCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("status") {
			update.Status = &updateStatus
		}
		if cmd.Flags().Changed("title") {
			update.Title = &updateTitle
		}
		if cmd.Flags().Changed("requested-by") {
			update.RequestedBy = &updateRequestedBy
		}
		if cmd.Flags().Changed("priority") {
			v, _ := cmd.Flags().GetInt("priority")
			update.Priority = &v
		}
		if cmd.Flags().Changed("description") {
			update.Body = &desc
		}
		return nil
	}
	link := func(add bool) *cobra.Command {
		verb := "unlink"
		if add {
			verb = "link"
		}
		return &cobra.Command{Use: verb + " <request-id> <issue-id...>", Short: verb + " issue links", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(true, false); err != nil {
				return err
			}
			op := ops.RequestUnlink(rs.opsEnv(), args[0], args[1:])
			if add {
				op = ops.RequestLink(rs.opsEnv(), args[0], args[1:])
			}
			out, err := rs.mutate(cmd.Context(), op)
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, out, verb+" "+args[0])
		}}
	}
	root.AddCommand(newCreate, list, show, updateCmd, link(true), link(false))
	return root
}

func validateRequestUpdate(in *ops.RequestUpdateInput) error {
	if in.Status != nil && !issue.ValidRequestStatus(*in.Status) {
		return fmt.Errorf("invalid request status %q", *in.Status)
	}
	return nil
}
