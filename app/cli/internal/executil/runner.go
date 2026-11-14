package executil

import (
	"context"
	"os/exec"
)

// CommandRunner runs a command and returns its standard output.
type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
