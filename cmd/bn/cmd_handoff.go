package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

type handoffJSON struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Issue         string         `json:"issue"`
	Created       time.Time      `json:"created"`
	Updated       time.Time      `json:"updated"`
	Project       string         `json:"project"`
	Path          string         `json:"path"`
	Archived      bool           `json:"archived"`
	Body          string         `json:"body,omitempty"`
	Backlinks     []backlinkJSON `json:"backlinks,omitempty"`
	ResolvedIssue *issueJSON     `json:"resolved_issue,omitempty"`
}

func toHandoffJSON(ix *vault.Index, h *issue.Handoff) handoffJSON {
	out := handoffJSON{ID: h.ID, Title: h.Title, Issue: h.Issue.Target, Created: h.Created, Updated: h.Updated, Project: h.Project, Path: h.Path, Archived: h.Archived}
	if ix == nil {
		return out
	}
	if n, ok := ix.Lookup(h.Issue.Target); ok && n.Issue != nil {
		out.Issue = n.Issue.ID
		summary := toIssueJSON(ix, n.Issue)
		out.ResolvedIssue = &summary
	}
	if n, ok := ix.ByPath[h.Path]; ok {
		for _, r := range ix.Backlinks[n.Basename] {
			out.Backlinks = append(out.Backlinks, backlinkJSON{From: r.From, Kind: string(r.Kind)})
		}
	}
	return out
}

func newHandoffCmd(rs *appState) *cobra.Command {
	root := &cobra.Command{Use: "handoff", Short: "Create and manage versioned session handoffs"}
	root.AddCommand(newHandoffCreateCmd(rs), newHandoffListCmd(rs), newHandoffShowCmd(rs), newHandoffAttachCmd(rs), newHandoffDetachCmd(rs), newHandoffArchiveCmd(rs), newHandoffRestoreCmd(rs))
	return root
}
func newHandoffCreateCmd(rs *appState) *cobra.Command {
	var file, issueID string
	var silent bool
	c := &cobra.Command{Use: "create [title]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if file == "" {
			return fmt.Errorf("--file is required")
		}
		var b []byte
		var e error
		if file == "-" {
			b, e = io.ReadAll(cmd.InOrStdin())
		} else {
			st, x := os.Stat(file)
			if x != nil {
				return x
			}
			if st.IsDir() {
				return fmt.Errorf("--file must name a file")
			}
			b, e = os.ReadFile(file)
		}
		if e != nil {
			return e
		}
		body := string(b)
		if strings.Contains(body, "\r\n") {
			return fmt.Errorf("handoff source has Windows line endings (\\r\\n)")
		}
		title := ""
		if len(args) > 0 {
			title = args[0]
		} else {
			title = handoffTitle(body)
		}
		if title == "" {
			return fmt.Errorf("title is required (pass [title] or include an H1 in --file)")
		}
		if err := rs.setupProject(true, false); err != nil {
			return err
		}
		op, out := ops.HandoffCreate(rs.opsEnv(), ops.HandoffCreateInput{Title: title, Body: body, Issue: issueID}, rs.prefixFor(rs.resolved.Project))
		res, e := rs.mutate(cmd.Context(), op)
		if e != nil {
			return e
		}
		if silent {
			fmt.Fprintln(cmd.OutOrStdout(), out.ID)
			return nil
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"id": out.ID, "path": out.Path, "project": rs.resolved.Project, "issue": issueID, "commit": res.SHA, "pushed": res.Pushed, "message": res.Message})
		}
		return rs.printCommit(cmd, res, "created handoff "+out.ID)
	}}
	c.Flags().StringVar(&file, "file", "", "Markdown source file or - for stdin")
	c.Flags().StringVar(&issueID, "issue", "", "attach to issue ID")
	c.Flags().BoolVar(&silent, "silent", false, "print only the handoff ID")
	return c
}
func handoffTitle(s string) string {
	fenced := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}
func newHandoffListCmd(rs *appState) *cobra.Command {
	var archived, all bool
	var issueID, older, sortBy string
	var limit int
	c := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rs.setupProject(false, all); err != nil {
			return err
		}
		ix, e := rs.readIndex(cmd.Context())
		if e != nil {
			return e
		}
		selectedIssue := issueID
		if issueID != "" {
			if iss, ok := ix.IssueByID(issueID); ok {
				selectedIssue = iss.ID
			} else {
				if n, found := ix.Lookup(issueID); !found || n.Issue == nil {
					return fmt.Errorf("%w: %s", ops.ErrNotFound, issueID)
				} else {
					selectedIssue = n.Issue.ID
				}
			}
		}
		age := time.Duration(0)
		if older != "" {
			age, e = parseAge(older)
			if e != nil {
				return e
			}
			if age < 0 {
				return fmt.Errorf("--older-than must not be negative")
			}
		}
		if sortBy != "created" && sortBy != "updated" {
			return fmt.Errorf("--sort must be created or updated")
		}
		if limit < 0 {
			return fmt.Errorf("--limit must not be negative")
		}
		out := []handoffJSON{}
		for _, h := range ix.ProjectHandoffs(rs.projectScope(all), archived) {
			if issueID != "" && h.Issue.Target != selectedIssue {
				if n, ok := ix.Lookup(h.Issue.Target); !ok || n.Issue == nil || n.Issue.ID != selectedIssue {
					continue
				}
			}
			if older != "" && h.Updated.After(time.Now().Add(-age)) {
				continue
			}
			out = append(out, toHandoffJSON(ix, h))
		}
		if sortBy == "updated" {
			sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
		}
		if limit > 0 && len(out) > limit {
			out = out[:limit]
		}
		if rs.jsonOut {
			return writeJSON(out)
		}
		for _, h := range out {
			project := ""
			if all {
				project = "\t" + h.Project
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s%s\n", h.ID, h.Created.Format("2006-01-02"), orDash(h.Issue), h.Title, project)
		}
		return nil
	}}
	c.Flags().BoolVar(&archived, "archived", false, "include archived handoffs")
	c.Flags().BoolVar(&all, "all-projects", false, "all projects")
	c.Flags().StringVar(&issueID, "issue", "", "only handoffs attached to issue")
	c.Flags().StringVar(&older, "older-than", "", "updated before age")
	c.Flags().StringVar(&sortBy, "sort", "created", "created or updated")
	c.Flags().IntVar(&limit, "limit", 50, "maximum entries (0 = all)")
	return c
}
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
func newHandoffShowCmd(rs *appState) *cobra.Command {
	var raw bool
	c := &cobra.Command{Use: "show <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if raw && rs.jsonOut {
			return fmt.Errorf("--raw cannot be used with --json")
		}
		if err := rs.setupProject(false, true); err != nil {
			return err
		}
		ix, e := rs.readIndex(cmd.Context())
		if e != nil {
			return e
		}
		h, ok := ix.HandoffByID(args[0])
		if !ok {
			return fmt.Errorf("%w: handoff %s", ops.ErrNotFound, args[0])
		}
		if raw {
			b, e := os.ReadFile(rs.paths.Hub + "/" + h.Path)
			if e != nil {
				return e
			}
			_, e = cmd.OutOrStdout().Write(b)
			return e
		}
		j := toHandoffJSON(ix, h)
		j.Body = h.Body
		if rs.jsonOut {
			return writeJSON(j)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n\n%s", h.ID, h.Title, h.Body)
		return nil
	}}
	c.Flags().BoolVar(&raw, "raw", false, "write exact stored Markdown")
	return c
}
func newHandoffAttachCmd(rs *appState) *cobra.Command {
	return &cobra.Command{Use: "attach <handoff-id> <issue-id>", Args: cobra.ExactArgs(2), RunE: func(c *cobra.Command, a []string) error {
		if e := rs.setupProject(false, true); e != nil {
			return e
		}
		r, e := rs.mutate(c.Context(), ops.HandoffAttach(rs.opsEnv(), a[0], a[1]))
		if e != nil {
			return e
		}
		return rs.printCommit(c, r, "attached handoff "+a[0])
	}}
}
func newHandoffDetachCmd(rs *appState) *cobra.Command {
	return &cobra.Command{Use: "detach <handoff-id>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		if e := rs.setupProject(false, true); e != nil {
			return e
		}
		r, e := rs.mutate(c.Context(), ops.HandoffDetach(rs.opsEnv(), a[0]))
		if e != nil {
			return e
		}
		return rs.printCommit(c, r, "detached handoff "+a[0])
	}}
}
func newHandoffArchiveCmd(rs *appState) *cobra.Command {
	var older string
	var all, dry bool
	c := &cobra.Command{Use: "archive [id...]", RunE: func(cmd *cobra.Command, a []string) error {
		if (len(a) == 0) == (older == "") {
			return fmt.Errorf("pass handoff IDs or --older-than")
		}
		if len(a) > 0 && all {
			return fmt.Errorf("--all-projects requires --older-than")
		}
		age := time.Duration(0)
		var e error
		if older != "" {
			age, e = parseAge(older)
			if e != nil {
				return e
			}
			if age < 0 {
				return fmt.Errorf("--older-than must not be negative")
			}
		}
		if e = rs.setupProject(false, all || len(a) > 0); e != nil {
			return e
		}
		op, out := ops.HandoffArchive(rs.opsEnv(), ops.HandoffArchiveInput{IDs: a, OlderThan: age, Project: rs.projectScope(all), DryRun: dry})
		var rsha string
		var pushed bool
		if dry {
			_, e = op.Apply(rs.paths.Hub)
		} else {
			var r gitops.Result
			r, e = rs.mutate(cmd.Context(), op)
			rsha = r.SHA
			pushed = r.Pushed
		}
		if e != nil {
			return e
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"moved": out.Moved, "already_archived": out.AlreadyArchived, "dry_run": dry, "commit": rsha, "pushed": pushed})
		}
		if len(out.Moved) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "nothing to archive")
		} else {
			verb := "archived"
			if dry {
				verb = "would archive"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, strings.Join(out.Moved, ", "))
		}
		return nil
	}}
	c.Flags().StringVar(&older, "older-than", "", "age")
	c.Flags().BoolVar(&all, "all-projects", false, "all projects")
	c.Flags().BoolVar(&dry, "dry-run", false, "report without moving")
	return c
}
func newHandoffRestoreCmd(rs *appState) *cobra.Command {
	return &cobra.Command{Use: "restore <id...>", Args: cobra.MinimumNArgs(1), RunE: func(c *cobra.Command, a []string) error {
		if e := rs.setupProject(false, true); e != nil {
			return e
		}
		op, out := ops.HandoffRestore(rs.opsEnv(), a)
		r, e := rs.mutate(c.Context(), op)
		if e != nil {
			return e
		}
		if rs.jsonOut {
			return writeJSON(map[string]any{"restored": out.Moved, "already_live": out.AlreadyLive, "commit": r.SHA, "pushed": r.Pushed})
		}
		return rs.printCommit(c, r, "restored handoffs")
	}}
}
