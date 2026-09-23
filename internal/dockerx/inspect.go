package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
)

// InspectContainer returns full container details from the daemon.
func (c *Client) InspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	out, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("inspect %s: %w", shortID(id), err)
	}
	return out, nil
}
