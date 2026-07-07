package hook

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Runner struct {
	Env map[string]string
}

func NewRunner() *Runner {
	return &Runner{Env: make(map[string]string)}
}

func (r *Runner) WithEnv(key, value string) *Runner {
	r.Env[key] = value
	return r
}

func (r *Runner) RunAll(ctx context.Context, cmds []string) error {
	for i, cmdStr := range cmds {
		if err := r.Run(ctx, cmdStr); err != nil {
			return fmt.Errorf("hook %d: %w", i, err)
		}
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, cmdStr string) error {
	if cmdStr == "" {
		return nil
	}

	var cmd *exec.Cmd
	if strings.ContainsAny(cmdStr, "|><&;") {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdStr)
	} else {
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			return nil
		}
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for k, v := range r.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	return cmd.Run()
}
