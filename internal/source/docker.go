package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

type DockerSource struct {
	volume string
}

func NewDockerSource(volume string) *DockerSource {
	return &DockerSource{volume: volume}
}

func (s *DockerSource) Type() string { return "docker" }

func (s *DockerSource) Name() string { return "docker:" + s.volume }

func (s *DockerSource) Capture(ctx context.Context) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", s.volume+":/from:ro",
		"busybox:latest",
		"tar", "cf", "-", "-C", "/from", ".")

	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting docker: %w", err)
	}

	return &dockerReadCloser{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

type dockerReadCloser struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
}

func (c *dockerReadCloser) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *dockerReadCloser) Close() error {
	err := c.cmd.Wait()
	if err != nil {
		if c.stderr.Len() > 0 {
			return fmt.Errorf("docker failed: %s", c.stderr.String())
		}
		return fmt.Errorf("docker failed: %w", err)
	}
	return nil
}
