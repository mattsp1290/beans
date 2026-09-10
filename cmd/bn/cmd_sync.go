package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSyncCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Pull the hub with rebase and push local commits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupHub(); err != nil {
				return err
			}
			if err := rs.hub.Sync(cmd.Context()); err != nil {
				return err
			}
			st, err := rs.hub.Status(cmd.Context())
			if err != nil {
				return err
			}
			if rs.jsonOut {
				return writeJSON(map[string]any{"ahead": st.Ahead, "behind": st.Behind})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "hub in sync with %s (ahead %d, behind %d)\n", st.Remote, st.Ahead, st.Behind)
			return nil
		},
	}
}
