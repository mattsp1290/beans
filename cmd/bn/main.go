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

// run executes the CLI and returns any error. Separating run from main keeps
// deferred cleanup on every exit path; os.Exit does not run deferred functions.
func run() error {
	rs := &appState{}
	return fang.Execute(
		context.Background(), newRootCmd(rs),
		fang.WithVersion(version.Version),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
}
