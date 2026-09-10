package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/vault"
)

type docJSON struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Project string `json:"project"`
}

func newDocCmd(rs *appState) *cobra.Command {
	doc := &cobra.Command{Use: "doc", Short: "Wiki pages under docs/"}
	var global bool
	newDoc := &cobra.Command{
		Use:   "new <path>",
		Short: "Create docs/<path>.md from templates/doc.md or a title line",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(!global, global); err != nil {
				return err
			}
			op, rel := ops.DocNew(rs.opsEnv(), args[0], global)
			res, err := rs.mutate(cmd.Context(), op)
			if err != nil {
				return err
			}
			if rs.jsonOut {
				return writeJSON(map[string]any{"path": rel, "commit": res.SHA, "pushed": res.Pushed, "message": res.Message})
			}
			return rs.printCommit(cmd, res, "created "+rel)
		},
	}
	newDoc.Flags().BoolVar(&global, "global", false, "hub-level docs/ instead of the project's")
	var listGlobal bool
	list := &cobra.Command{
		Use:   "list [dir]",
		Short: "List docs with their titles",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			prefix := ""
			if len(args) == 1 {
				prefix = strings.Trim(args[0], "/")
			}
			var out []docJSON
			for _, n := range ix.Notes {
				if n.Kind != vault.KindDoc {
					continue
				}
				if listGlobal && n.Project != "" {
					continue
				}
				if !listGlobal && rs.resolved.Project != "" && n.Project != "" && n.Project != rs.resolved.Project {
					continue
				}
				if prefix != "" && !strings.Contains(n.Path, prefix+"/") && !strings.HasPrefix(n.Path, prefix) {
					continue
				}
				out = append(out, docJSON{Path: n.Path, Title: n.Title, Project: n.Project})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
			if rs.jsonOut {
				if out == nil {
					out = []docJSON{}
				}
				return writeJSON(out)
			}
			w := tableWriter(cmd)
			for _, d := range out {
				fmt.Fprintf(w, "%s\t%s\n", d.Path, d.Title)
			}
			return w.Flush()
		},
	}
	list.Flags().BoolVar(&listGlobal, "global", false, "hub-level docs only")
	backlinks := &cobra.Command{
		Use:   "backlinks <path>",
		Short: "Notes that link to a doc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			n, ok := ix.Lookup(args[0])
			if !ok {
				return notFound("doc %s not found", args[0])
			}
			refs := append([]vault.LinkRef(nil), ix.Backlinks[n.Basename]...)
			sort.Slice(refs, func(i, j int) bool { return refs[i].From < refs[j].From })
			if rs.jsonOut {
				out := make([]backlinkJSON, 0, len(refs))
				for _, r := range refs {
					bl := backlinkJSON{From: r.From, Kind: string(r.Kind)}
					if fn, ok := ix.Notes[r.From]; ok {
						bl.Path = fn.Path
					}
					out = append(out, bl)
				}
				return writeJSON(out)
			}
			for _, r := range refs {
				fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", r.From, r.Kind)
			}
			return nil
		},
	}
	doc.AddCommand(newDoc, list, backlinks)
	return doc
}
