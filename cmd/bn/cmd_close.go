package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	store "github.com/mattsp1290/beans/store"
)

func newCloseCmd(rs *appState) *cobra.Command {
	var (
		reason string
		force  bool // accepted for bd compat; no special behavior in bn
	)

	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close an issue (idempotent)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.requirePrefix(); err != nil {
				return err
			}

			id := args[0]
			_ = force // bd compat shim: CloseIssue is already idempotent, nothing to force

			err := rs.store.CloseIssue(cmd.Context(), id, rs.actor, reason)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					fmt.Fprintf(cmd.ErrOrStderr(), "not found: %s\n", id)
					return fmt.Errorf("not found: %s", id)
				}
				return fmt.Errorf("close: %w", err)
			}

			if !rs.jsonOut {
				fmt.Fprintf(cmd.OutOrStdout(), "Closed %s\n", id)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&reason, "reason", "r", "", "reason for closing (recorded as a note)")
	cmd.Flags().BoolVar(&force, "force", false, "accepted for bd compat")
	return cmd
}
