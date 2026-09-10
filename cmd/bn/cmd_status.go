package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/vault"
)

type statusJSON struct {
	Hub        string   `json:"hub"`
	Remote     string   `json:"remote"`
	Branch     string   `json:"branch"`
	Ahead      int      `json:"ahead"`
	Behind     int      `json:"behind"`
	Dirty      []string `json:"dirty"`
	LastFetch  string   `json:"last_fetch,omitempty"`
	Project    string   `json:"project,omitempty"`
	ProjectDir string   `json:"project_dir,omitempty"`
	Resolution string   `json:"resolution"`
	RepoRoot   string   `json:"repo_root,omitempty"`
	RepoRemote string   `json:"repo_remote,omitempty"`
	RepoHead   string   `json:"repo_head,omitempty"`
	RepoBranch string   `json:"repo_branch,omitempty"`
}

func newStatusCmd(rs *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the hub clone state and the resolved project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupHub(); err != nil {
				return err
			}
			rs.fetch(cmd.Context())
			st, err := rs.hub.Status(cmd.Context())
			if err != nil {
				return err
			}
			out := statusJSON{Hub: st.Dir, Remote: st.Remote, Branch: st.Branch, Ahead: st.Ahead, Behind: st.Behind, Dirty: st.Dirty}
			if out.Dirty == nil {
				out.Dirty = []string{}
			}
			if !st.LastFetch.IsZero() {
				out.LastFetch = st.LastFetch.UTC().Format(time.RFC3339)
			}
			res, rerr := vault.Resolve(rs.paths.Hub, vault.ResolveOptions{Cwd: rs.cwd, FlagProject: rs.project, Git: rs.git, Env: rs.env})
			switch {
			case rerr == nil && res.Project != "":
				out.Project, out.ProjectDir = res.Project, res.ProjectDir
				out.Resolution = "resolved"
				if res.Notice != "" {
					out.Resolution = res.Notice
				}
			case errors.Is(rerr, vault.ErrOutsideRepo):
				out.Resolution = rerr.Error()
			case rerr != nil:
				out.Resolution = rerr.Error()
			default:
				out.Resolution = "none"
			}
			if rerr == nil {
				out.RepoRoot, out.RepoRemote, out.RepoHead, out.RepoBranch = res.RepoRoot, res.RepoRemote, res.RepoHead, res.RepoBranch
			}
			if rs.jsonOut {
				return writeJSON(out)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "hub:        %s\nremote:     %s\nbranch:     %s\nahead:      %d\nbehind:     %d\n", out.Hub, out.Remote, out.Branch, out.Ahead, out.Behind)
			if len(out.Dirty) == 0 {
				fmt.Fprintln(w, "dirty:      none")
			} else {
				fmt.Fprintf(w, "dirty:      %d path(s)\n", len(out.Dirty))
				for _, d := range out.Dirty {
					fmt.Fprintf(w, "            %s\n", d)
				}
			}
			if st.LastFetch.IsZero() {
				fmt.Fprintln(w, "last fetch: never")
			} else {
				fmt.Fprintf(w, "last fetch: %s ago\n", time.Since(st.LastFetch).Round(time.Second))
			}
			if out.Project != "" {
				fmt.Fprintf(w, "project:    %s (%s)\n", out.Project, out.Resolution)
			} else {
				fmt.Fprintf(w, "project:    none (%s)\n", out.Resolution)
			}
			if out.RepoRoot != "" {
				fmt.Fprintf(w, "repo:       %s", out.RepoRoot)
				if out.RepoHead != "" {
					fmt.Fprintf(w, " @%s", out.RepoHead)
				}
				if out.RepoBranch != "" {
					fmt.Fprintf(w, " %s", out.RepoBranch)
				}
				fmt.Fprintln(w)
			}
			return nil
		},
	}
}
