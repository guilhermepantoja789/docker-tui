package dockerx

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
)

// LogOptions configures ContainerLogs.
type LogOptions struct {
	Follow     bool
	Tail       string
	Since      string
	Timestamps bool
	Stdout     bool
	Stderr     bool
}

// ContainerLogs opens a log stream for a container.
// The caller must Close the returned ReadCloser; cancel ctx to stop follow.
// The stream is Docker-multiplexed unless the container was started with a TTY.
func (c *Client) ContainerLogs(ctx context.Context, id string, opts LogOptions) (io.ReadCloser, error) {
	if !opts.Stdout && !opts.Stderr {
		opts.Stdout = true
		opts.Stderr = true
	}
	r, err := c.api.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: opts.Stdout,
		ShowStderr: opts.Stderr,
		Follow:     opts.Follow,
		Tail:       opts.Tail,
		Since:      opts.Since,
		Timestamps: opts.Timestamps,
	})
	if err != nil {
		return nil, fmt.Errorf("logs %s: %w", shortID(id), err)
	}
	return r, nil
}
