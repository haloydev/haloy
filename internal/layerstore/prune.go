package layerstore

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/storage"
)

func PruneUnusedLayers(ctx context.Context, cli *client.Client, logger *slog.Logger) (int, int64, error) {
	db, err := storage.New()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	store, err := New(db)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create layer store: %w", err)
	}

	appNames, err := db.ListDistinctAppNames()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list app names: %w", err)
	}

	neededDigests := make(map[string]struct{})
	for _, appName := range appNames {
		images, err := cli.ImageList(ctx, image.ListOptions{
			Filters: filters.NewArgs(filters.Arg("reference", appName+":*")),
		})
		if err != nil {
			return 0, 0, fmt.Errorf("failed to list images for %s: %w", appName, err)
		}

		for _, img := range images {
			inspect, err := cli.ImageInspect(ctx, img.ID)
			if err != nil {
				logger.Warn("Failed to inspect image, skipping", "imageID", img.ID, "error", err)
				continue
			}
			for _, diffID := range inspect.RootFS.Layers {
				neededDigests[diffID] = struct{}{}
			}
		}
	}

	allLayers, err := db.ListAllLayers()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list layers: %w", err)
	}

	cutoff := time.Now().Add(-1 * time.Hour)
	var pruned int
	var freed int64
	for _, layer := range allLayers {
		if _, needed := neededDigests[layer.Digest]; needed {
			continue
		}
		if layer.LastUsedAt.After(cutoff) {
			continue
		}
		if err := store.DeleteLayer(layer.Digest); err != nil {
			logger.Warn("Failed to delete layer", "digest", layer.Digest, "error", err)
			continue
		}
		pruned++
		freed += layer.Size
	}

	return pruned, freed, nil
}
