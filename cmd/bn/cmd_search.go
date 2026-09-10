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
	cmd := &cobra.Command{
		Use:   "search <query...>",
		Short: "Search issues, docs, and memories",
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
				kinds = []vault.Kind{vault.Kind(kind)}
			}
			hits := ix.Search(strings.Join(args, " "), kinds)
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
	cmd.Flags().StringVar(&kind, "kind", "", "issue, doc, or memory")
	cmd.Flags().BoolVar(&all, "all-projects", false, "every project in the hub")
	return cmd
}
