package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/docker"
)

func getAppContainers(ctx context.Context, appName string) (*client.Client, []container.Summary, error) {
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	containerList, err := docker.GetAppContainers(ctx, cli, false, appName)
	if err != nil {
		cli.Close()
		return nil, nil, err
	}

	if len(containerList) == 0 {
		cli.Close()
		return nil, nil, fmt.Errorf("no running containers found for the specified app")
	}

	return cli, containerList, nil
}

func selectContainers(containerList []container.Summary, containerID string, allContainers bool) ([]string, error) {
	if containerID != "" && allContainers {
		return nil, fmt.Errorf("cannot specify both containerId and allContainers")
	}

	var targetIDs []string

	switch {
	case containerID != "":
		for _, c := range containerList {
			if c.ID == containerID || strings.HasPrefix(c.ID, containerID) {
				targetIDs = append(targetIDs, c.ID)
				return targetIDs, nil
			}
		}
		return nil, fmt.Errorf("specified container not found for this app")
	case allContainers:
		for _, c := range containerList {
			targetIDs = append(targetIDs, c.ID)
		}
	default:
		targetIDs = append(targetIDs, containerList[0].ID)
	}

	return targetIDs, nil
}
