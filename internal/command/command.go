package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type RunOptions struct {
	Dir   string
	Env   []string
	Stdin []byte
}

type Runner interface {
	Run(ctx context.Context, name string, args []string, opts RunOptions) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, opts RunOptions) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = opts.Dir
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Environ(), opts.Env...)
	}
	if len(opts.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %v: %w\n%s", name, args, err, bytes.TrimSpace(out))
	}
	return out, nil
}
