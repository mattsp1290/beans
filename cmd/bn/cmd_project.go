package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

type projectJSON struct {
	Name    string   `json:"name"`
	Prefix  string   `json:"prefix"`
	Remotes []string `json:"remotes"`
	Dir     string   `json:"dir"`
}

func newProjectCmd(rs *appState) *cobra.Command {
	project := &cobra.Command{
		Use:   "project",
		Short: "List, inspect, create, and link projects in the hub",
	}

	project.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the projects in the hub",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupHub(); err != nil {
				return err
			}
			rs.fetch(cmd.Context())
			names, err := vault.ProjectDirs(rs.paths.Hub)
			if err != nil {
				return err
			}
			var out []projectJSON
			for _, n := range names {
				pj, err := rs.projectJSON(n)
				if err != nil {
					return err
				}
				out = append(out, pj)
			}
			if rs.jsonOut {
				if out == nil {
					out = []projectJSON{}
				}
				return writeJSON(out)
			}
			for _, p := range out {
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s prefix=%s remotes=%s\n", p.Name, p.Prefix, strings.Join(p.Remotes, ","))
			}
			return nil
		},
	})

	project.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show a project's configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rs.setupHub(); err != nil {
				return err
			}
			rs.fetch(cmd.Context())
			pj, err := rs.projectJSON(args[0])
			if err != nil {
				return err
			}
			if rs.jsonOut {
				return writeJSON(pj)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "name:    %s\nprefix:  %s\nremotes: %s\ndir:     %s\n", pj.Name, pj.Prefix, strings.Join(pj.Remotes, ", "), pj.Dir)
			return nil
		},
	})

	var link bool
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a project in the hub (one commit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !vault.ValidProjectName(name) {
				return fmt.Errorf("invalid project name %q (use [a-z0-9-])", name)
			}
			if err := rs.setupHub(); err != nil {
				return err
			}
			remote := ""
			if link {
				res, err := vault.Resolve(rs.paths.Hub, vault.ResolveOptions{Cwd: rs.cwd, FlagProject: name, Write: true, Git: rs.git, Env: rs.env})
				if err != nil {
					return err
				}
				if res.RepoRemote == "" {
					return fmt.Errorf("--link needs a git repository with an origin remote in the current directory")
				}
				remote = res.RepoRemote
			}
			if _, err := os.Stat(filepath.Join(rs.paths.Hub, "projects", name, "beans.toml")); err == nil {
				return fmt.Errorf("project %s already exists; use bn project link %s to add this repository's remote", name, name)
			}
			res, err := rs.mutate(cmd.Context(), gitops.Operation{Verb: "project create", ID: name, Summary: name, Apply: func(hubDir string) ([]string, error) {
				return vault.CreateProjectFiles(hubDir, name, remote)
			}})
			if err != nil {
				return err
			}
			return rs.printCommit(cmd, res, "created project "+name)
		},
	}
	create.Flags().BoolVar(&link, "link", false, "record the current repository's remote in the new project")
	project.AddCommand(create)

	project.AddCommand(&cobra.Command{
		Use:   "link <name>",
		Short: "Add the current repository's remote to a project's remotes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := rs.setupHub(); err != nil {
				return err
			}
			res, err := vault.Resolve(rs.paths.Hub, vault.ResolveOptions{Cwd: rs.cwd, FlagProject: name, Git: rs.git, Env: rs.env})
			if err != nil {
				return notFound("%v", err)
			}
			if res.RepoRemote == "" {
				return fmt.Errorf("bn project link needs a git repository with an origin remote in the current directory")
			}
			remote := res.RepoRemote
			rel := filepath.ToSlash(filepath.Join("projects", name, "beans.toml"))
			out, err := rs.mutate(cmd.Context(), gitops.Operation{Verb: "project link", ID: name, Summary: remote, Apply: func(hubDir string) ([]string, error) {
				path := filepath.Join(hubDir, rel)
				cfg, err := issue.LoadProjectConfig(path)
				if err != nil {
					return nil, err
				}
				for _, r := range cfg.Remotes {
					if n, err := vault.NormalizeRemoteURL(r); err == nil && n == remote {
						return nil, nil
					}
				}
				cfg.Remotes = append(cfg.Remotes, remote)
				data, err := issue.EncodeProjectConfig(cfg)
				if err != nil {
					return nil, err
				}
				if err := gitops.WriteFile(path, data); err != nil {
					return nil, err
				}
				return []string{rel}, nil
			}})
			if err != nil {
				return err
			}
			if out.SHA == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "project %s already linked to %s\n", name, remote)
				return nil
			}
			return rs.printCommit(cmd, out, "linked "+remote+" to project "+name)
		},
	})
	return project
}

func (rs *appState) projectJSON(name string) (projectJSON, error) {
	dir := filepath.Join(rs.paths.Hub, "projects", name)
	path := filepath.Join(dir, "beans.toml")
	if _, err := os.Stat(path); err != nil {
		return projectJSON{}, notFound("project %s does not exist in the hub", name)
	}
	cfg, err := issue.LoadProjectConfig(path)
	if err != nil {
		return projectJSON{}, err
	}
	remotes := cfg.Remotes
	if remotes == nil {
		remotes = []string{}
	}
	return projectJSON{Name: cfg.Name, Prefix: cfg.Prefix, Remotes: remotes, Dir: dir}, nil
}

// printCommit reports a mutation's outcome.
func (rs *appState) printCommit(cmd *cobra.Command, res gitops.Result, what string) error {
	if rs.jsonOut {
		return writeJSON(map[string]any{"commit": res.SHA, "pushed": res.Pushed, "message": res.Message})
	}
	if res.SHA == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s (no change)\n", what)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", what, res.SHA[:7])
	return nil
}
