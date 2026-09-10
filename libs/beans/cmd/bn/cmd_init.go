package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInitCmd(rs *appState) *cobra.Command {
	var (
		prefix         string
		nonInteractive bool
		quiet          bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project (register prefix, ensure schema)",
		Long: `Register a project prefix in the database and write a .bn project marker
to the current directory. --prefix is always required.

Under multi-repository topology, running bn create inside a git repo automatically
registers the repo and derives a project prefix from the remote URL (slug == prefix).
Use bn init --prefix when you need a custom or human-readable prefix that differs
from the auto-derived remote URL slug, or for non-git setups.`,
		Args: cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return rs.initConnForInit(cmd.Context())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if prefix == "" {
				return fmt.Errorf("--prefix is required")
			}
			if err := rs.store.EnsureProject(cmd.Context(), prefix); err != nil {
				return fmt.Errorf("init: %w", err)
			}
			if err := writeActiveProjectMarker(prefix); err != nil {
				return fmt.Errorf("init: %w", err)
			}
			if !quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "Initialized project %q\n", prefix)
			}
			_ = nonInteractive
			return nil
		},
	}

	cmd.Flags().StringVar(&prefix, "prefix", "", "project prefix to register (required)")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "disable interactive prompts")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress output on success")
	return cmd
}
