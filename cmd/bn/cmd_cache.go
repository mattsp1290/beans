package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/vault"
)

func newCacheCmd(rs *appState) *cobra.Command {
	cache := &cobra.Command{
		Use:   "cache",
		Short: "Manage ~/.beans/cache",
	}
	cache.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Remove derived data under ~/.beans/cache (keeps an active lock)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := vault.DefaultPaths(rs.hubFlag)
			if err != nil {
				return err
			}
			entries, err := os.ReadDir(paths.Cache)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "cache is empty")
					return nil
				}
				return err
			}
			var removed []string
			for _, e := range entries {
				if e.Name() == "hub.lock" {
					continue
				}
				p := filepath.Join(paths.Cache, e.Name())
				if err := os.RemoveAll(p); err != nil {
					return err
				}
				removed = append(removed, e.Name())
			}
			if rs.jsonOut {
				if removed == nil {
					removed = []string{}
				}
				return writeJSON(map[string]any{"removed": removed})
			}
			if len(removed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "cache is empty")
				return nil
			}
			for _, r := range removed {
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", r)
			}
			return nil
		},
	})
	return cache
}
