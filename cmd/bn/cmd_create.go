package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/ops"
)

func newCreateCmd(rs *appState) *cobra.Command {
	var in ops.CreateInput
	var silent bool
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create an issue in the current project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupProject(true, false); err != nil {
				return err
			}
			in.Title = args[0]
			if rs.resolved.Created {
				in.Remote = rs.resolved.RepoRemote
			}
			op, res := ops.Create(rs.opsEnv(), in, rs.prefixFor(rs.resolved.Project))
			out, err := rs.mutate(cmd.Context(), op)
			if err != nil {
				return err
			}
			switch {
			case rs.jsonOut:
				return writeJSON(map[string]any{"id": res.ID, "path": res.Path, "commit": out.SHA, "pushed": out.Pushed, "message": out.Message})
			case silent:
				fmt.Fprintln(cmd.OutOrStdout(), res.ID)
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "created %s: %s\n", res.ID, in.Title)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&in.Description, "description", "d", "", "description text (the body starts from the type template)")
	f.IntVarP(&in.Priority, "priority", "p", 2, "priority 0 (critical) to 4 (backlog)")
	f.StringVarP(&in.Type, "type", "t", "task", "issue type")
	f.StringArrayVarP(&in.Labels, "label", "l", nil, "label (repeatable)")
	f.StringVar(&in.Parent, "parent", "", "parent issue id")
	f.StringVar(&in.Assignee, "assignee", "", "assignee")
	f.StringArrayVar(&in.BlockedBy, "blocked-by", nil, "blocking issue id (repeatable)")
	f.StringVar(&in.URL, "url", "", "external URL")
	f.BoolVar(&silent, "silent", false, "print only the new id")
	return cmd
}
