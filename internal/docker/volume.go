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

// EnsureVolumes creates named volumes with labels if they don't exist.
// It parses volume specifications from the config (e.g., "postgres-data:/var/lib/postgresql")
// and creates the named volumes with the app label for later cleanup.
func EnsureVolumes(ctx context.Context, cli *client.Client, logger *slog.Logger, appName string, volumes []string) error {
	for _, volSpec := range volumes {
		parsed, err := config.ParseVolumeSpec(volSpec)
		if err != nil {
			return err
		}

		if !parsed.IsNamedVolume() {
			// Not a named volume (likely a bind mount), skip
			continue
		}

		volumeName := parsed.Source

		// Check if volume already exists
		_, err = cli.VolumeInspect(ctx, volumeName)
		if err == nil {
			// Volume exists, skip creation
			logger.Debug("Volume already exists", "volume", volumeName)
			continue
		}

		if !client.IsErrNotFound(err) {
			return fmt.Errorf("failed to inspect volume %s: %w", volumeName, err)
		}

		// Create volume with labels
		_, err = cli.VolumeCreate(ctx, volume.CreateOptions{
			Name: volumeName,
			Labels: map[string]string{
				config.LabelAppName: appName,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create volume %s: %w", volumeName, err)
		}
		logger.Info(fmt.Sprintf("Created volume for %s", appName), "volume", volumeName)
	}

	return nil
}

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
