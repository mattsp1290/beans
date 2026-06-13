package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newDeleteCmd(rs *appState) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Hard-delete an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			if !force {
				return fmt.Errorf("--force required to delete (this cannot be undone)")
			}

			id := args[0]
			err := rs.store.DeleteIssue(cmd.Context(), id)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					fmt.Fprintf(cmd.ErrOrStderr(), "not found: %s\n", id)
					return fmt.Errorf("not found: %s", id)
				}
				return fmt.Errorf("delete: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s\n", id)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "confirm permanent deletion (required)")
	return cmd
}
