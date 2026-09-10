package main

import (
	"context"
	"os"
	"syscall"

	"github.com/charmbracelet/fang"

	"github.com/mattsp1290/beans/version"
)

func main() {
	os.Exit(run())
}

// run executes the CLI and returns the process exit code. fang prints the
// error; the code comes from the error type (see exitCode).
func run() int {
	rs := &appState{}
	err := fang.Execute(
		context.Background(), newRootCmd(rs),
		fang.WithVersion(version.Version),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
	return exitCode(err)
}
