package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

func newInitCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "init <remote>",
		Short: "Clone the hub repository into ~/.beans/hub",
		Long: `Clone the hub repository. An empty remote receives an initial commit with
README.md, beans.toml, and the docs/, memories/, and projects/ directories.
The remote and its default branch are recorded in ~/.beans/config.toml.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			remote := args[0]
			paths, err := vault.DefaultPaths(rs.hubFlag)
			if err != nil {
				return err
			}
			if entries, err := os.ReadDir(paths.Hub); err == nil && len(entries) > 0 {
				if _, cerr := os.Stat(paths.Config); cerr != nil {
					return fmt.Errorf("%s already exists but no config was written (a previous bn init may have failed); remove the directory and retry, or set BEANS_HUB to use another directory", paths.Hub)
				}
				return fmt.Errorf("%s already exists and is not empty; remove it or set BEANS_HUB to use another directory", paths.Hub)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			branch, err := gitops.Clone(cmd.Context(), gitops.ExecRunner{}, remote, paths.Hub)
			if err != nil {
				return err
			}
			cfg, err := issue.LoadUserConfig(paths.Config)
			if err != nil {
				return err
			}
			cfg.Hub.Remote = remote
			cfg.Hub.Branch = branch
			data, err := issue.EncodeUserConfig(cfg)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(paths.Config), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(paths.Config, data, 0o644); err != nil {
				return err
			}
			if err := os.MkdirAll(paths.Cache, 0o755); err != nil {
				return err
			}
			if rs.jsonOut {
				return writeJSON(map[string]string{"hub": paths.Hub, "remote": remote, "branch": branch, "config": paths.Config})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "hub cloned to %s (branch %s)\nconfig written to %s\n", paths.Hub, branch, paths.Config)
			return nil
		},
	}
}
