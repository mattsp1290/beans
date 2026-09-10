package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes git in a directory. It is the seam tests use to inject
// faults between pipeline steps.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (stdout string, err error)
}

// ExecRunner runs the git binary on PATH. It inherits the user's environment
// so credential helpers and ssh agents work, and disables terminal prompts so
// a missing credential fails instead of hanging.
type ExecRunner struct{}

// GitError carries git's exit status and stderr.
type GitError struct {
	Args   []string
	Dir    string
	Code   int
	Stderr string
}

func (e *GitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.Code)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), &GitError{Args: args, Dir: dir, Code: exitErr.ExitCode(), Stderr: stderr.String()}
		}
		return stdout.String(), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// IsPushRejected reports whether err is a non-fast-forward push rejection.
func IsPushRejected(err error) bool {
	var ge *GitError
	if !errors.As(err, &ge) {
		return false
	}
	s := ge.Stderr
	return strings.Contains(s, "[rejected]") || strings.Contains(s, "non-fast-forward") || strings.Contains(s, "fetch first") || strings.Contains(s, "failed to push some refs")
}
