package database

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

type execAdapter struct {
	name     string
	cmd      string
	args     []string
	dsn      string
	parseDSN func(dsn string) (args []string)
}

func (e *execAdapter) Name() string { return e.name }

func (e *execAdapter) Dump(ctx context.Context) (io.ReadCloser, error) {
	args := e.args
	if e.parseDSN != nil {
		args = append(args, e.parseDSN(e.dsn)...)
	}

	cmd := exec.CommandContext(ctx, e.cmd, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", e.cmd, err)
	}

	return &cmdReadCloser{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

type cmdReadCloser struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
}

func (c *cmdReadCloser) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *cmdReadCloser) Close() error {
	err := c.cmd.Wait()
	if err != nil {
		if c.stderr.Len() > 0 {
			return fmt.Errorf("%s failed: %s", c.cmd.Path, c.stderr.String())
		}
		return fmt.Errorf("%s failed: %w", c.cmd.Path, err)
	}
	return nil
}
