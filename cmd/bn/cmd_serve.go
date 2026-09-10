package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/internal/server"
	"github.com/mattsp1290/beans/ui"
	"github.com/mattsp1290/beans/vault"
)

func newServeCmd(rs *appState) *cobra.Command {
	var port int
	var host string
	var open bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the issues board and wiki over the hub",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rs.setupProject(false, true); err != nil {
				return err
			}
			ix, err := rs.readIndex(cmd.Context())
			if err != nil {
				return err
			}
			dist, err := fs.Sub(ui.Dist, "dist")
			if err != nil {
				return err
			}
			srv := server.New(server.Config{
				Hub: rs.hub, Index: ix, HubDir: rs.paths.Hub, Project: rs.resolved.Project,
				Env: rs.opsEnv(), Prefix: rs.prefixFor, UI: dist, LogOutput: rs.stderr, NoFetch: rs.noFetch,
			})
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				err := vault.WatchWithOptions(ctx, ix, vault.WatchOptions{
					OnChange: srv.Notify,
					OnError:  func(err error) { fmt.Fprintf(rs.stderr, "bn serve: reload failed: %v\n", err) },
				})
				if err != nil && ctx.Err() == nil {
					fmt.Fprintf(rs.stderr, "bn serve: watcher stopped: %v\n", err)
				}
			}()
			addr := fmt.Sprintf("%s:%d", host, port)
			url := fmt.Sprintf("http://%s/", addr)
			if host == "0.0.0.0" {
				fmt.Fprintln(rs.stderr, "warning: serving on all interfaces with no authentication")
				url = fmt.Sprintf("http://127.0.0.1:%d/", port)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "bn serve: %s (hub %s, project %s)\n", url, rs.paths.Hub, rs.resolved.Project)
			if open {
				openBrowser(url)
			}
			return srv.Run(ctx, addr)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7777, "port to listen on")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "address to bind")
	cmd.Flags().BoolVar(&open, "open", false, "open the browser")
	return cmd
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

var _ = strings.TrimSpace
var _ = context.Background
