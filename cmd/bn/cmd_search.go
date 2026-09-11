package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/vault"
)

func newSearchCmd(rs *appState) *cobra.Command {
	var kind string
	var all bool
	var includeArchivedHandoffs bool
	cmd := &cobra.Command{
		Use:   "search <query...>",
		Short: "Search issues, requests, docs, and memories",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			var kinds []vault.Kind
			if kind != "" {
				if !map[vault.Kind]bool{vault.KindIssue: true, vault.KindRequest: true, vault.KindDoc: true, vault.KindMemory: true, vault.KindPlan: true, vault.KindHandoff: true}[vault.Kind(kind)] {
					return fmt.Errorf("unknown kind %q (valid: issue, request, doc, memory, plan, handoff)", kind)
				}
				kinds = []vault.Kind{vault.Kind(kind)}
			}
			hits := ix.SearchWithOptions(strings.Join(args, " "), vault.SearchOptions{Kinds: kinds, IncludeArchivedHandoffs: includeArchivedHandoffs})
			project := rs.projectScope(all)
			var out []vault.Hit
			for _, h := range hits {
				if project != "" && h.Project != "" && h.Project != project {
					continue
				}
				out = append(out, h)
			}
			if rs.jsonOut {
				if out == nil {
					out = []vault.Hit{}
				}
				return writeJSON(out)
			}
			w := tableWriter(cmd)
			for _, h := range out {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", h.Kind, h.ID, h.Title, h.Path)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "issue, request, doc, memory, plan, or handoff")
	cmd.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	cmd.Flags().BoolVar(&includeArchivedHandoffs, "include-archived-handoffs", false, "include historical handoffs")
	return cmd
}
