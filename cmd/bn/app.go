package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
)

// appState carries the flag values and injectable seams shared by every
// subcommand. Later work packages add hub and project resolution here.
type appState struct {
	// resolved from flags / env
	project string
	actor   string
	jsonOut bool

	// injectable seam for git workspace queries; defaults to gitops.SystemGit
	git gitops.Resolver

	// injectable seam for diagnostic warnings; defaults to os.Stderr
	stderr io.Writer
}

func newRootCmd(rs *appState) *cobra.Command {
	if rs.git == nil {
		rs.git = gitops.SystemGit{}
	}
	if rs.stderr == nil {
		rs.stderr = os.Stderr
	}
	root := &cobra.Command{
		Use:           "bn",
		Short:         "Git-backed issue tracker and wiki (bn = beans)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.PersistentFlags().StringVar(&rs.project, "project", "", "project name (overrides $BEANS_PROJECT and git auto-detection)")
	root.PersistentFlags().StringVar(&rs.actor, "actor", "", "audit actor (overrides $BN_ACTOR)")
	root.PersistentFlags().BoolVar(&rs.jsonOut, "json", false, "machine-readable JSON output")

	return root
}

// resolveActor applies the actor precedence: --actor > BN_ACTOR >
// git config user.name > USER. The user config file joins the chain in WP2.
func (rs *appState) resolveActor() string {
	if rs.actor != "" {
		return rs.actor
	}
	if v := os.Getenv("BN_ACTOR"); v != "" {
		rs.actor = v
		return rs.actor
	}
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			rs.actor = name
			return rs.actor
		}
	}
	rs.actor = os.Getenv("USER")
	return rs.actor
}

// ---------------------------------------------------------------------------
// JSON output helpers
// ---------------------------------------------------------------------------

func writeJSON(v any) error {
	return writeJSONTo(os.Stdout, v)
}

func writeJSONTo(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
