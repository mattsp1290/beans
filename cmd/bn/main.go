package main

import (
	"context"
	"os"
	"syscall"

	"github.com/charmbracelet/fang"

	"github.com/mattsp1290/beans/version"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run executes the CLI and returns any error. Separating run from main ensures
// deferred cleanup (store.Close) runs on all exit paths, including errors —
// os.Exit does not run deferred functions.
func run() error {
	rs := &appState{}
	defer func() {
		if rs.store != nil {
			rs.store.Close()
		}
	}()

	return fang.Execute(
		context.Background(), newRootCmd(rs),
		fang.WithVersion(version.Version),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
}
