package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/client"
)

func NewClient(ctx context.Context) (*client.Client, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	_, err = cli.Ping(pingCtx)
	if err != nil {
		_ = cli.Close() // Best effort close, ignore error
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("failed to connect to Docker daemon (timeout during ping): %w", err)
		}
		return nil, fmt.Errorf("failed to connect to Docker daemon (ping failed): %w", err)
	}
	return cli, nil
}
