package docker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
)

func RemoveVolumes(ctx context.Context, cli *client.Client, logger *slog.Logger, appName string) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))

	volumeList, err := cli.VolumeList(ctx, volume.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return fmt.Errorf("failed to list volumes for app %s: %w", appName, err)
	}

	for _, vol := range volumeList.Volumes {
		err := cli.VolumeRemove(ctx, vol.Name, true) // true = force removal
		if err != nil {
			// If already removed, that's fine
			if client.IsErrNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to remove volume %s: %w", vol.Name, err)
		}
	}

	return nil
}
