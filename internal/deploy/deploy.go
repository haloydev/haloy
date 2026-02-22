package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/storage"
)

type deployDockerOps interface {
	EnsureImageUpToDate(ctx context.Context, logger *slog.Logger, imageConfig config.Image) error
	TagImage(ctx context.Context, srcRef, appName, deploymentID string) (string, error)
	StopContainers(ctx context.Context, logger *slog.Logger, appName, ignoreDeploymentID string) ([]string, error)
	RemoveContainers(ctx context.Context, logger *slog.Logger, appName, ignoreDeploymentID string) ([]string, error)
	EnsureVolumes(ctx context.Context, logger *slog.Logger, appName string, volumes []string) error
	RunContainer(ctx context.Context, deploymentID, imageRef string, targetConfig config.TargetConfig) ([]docker.ContainerRunResult, error)
	RemoveImages(ctx context.Context, logger *slog.Logger, appName, ignoreDeploymentID string, deploymentsToKeep int) error
}

type deploymentStore interface {
	SaveDeployment(deployment storage.Deployment) error
	PruneOldDeployments(appName string, deploymentsToKeep int) error
	Close() error
}

type deploymentStoreFactory interface {
	Open() (deploymentStore, error)
}

type runtimeDockerOps struct {
	cli *client.Client
}

func (r runtimeDockerOps) EnsureImageUpToDate(ctx context.Context, logger *slog.Logger, imageConfig config.Image) error {
	return docker.EnsureImageUpToDate(ctx, r.cli, logger, imageConfig)
}

func (r runtimeDockerOps) TagImage(ctx context.Context, srcRef, appName, deploymentID string) (string, error) {
	dstRef := fmt.Sprintf("%s:%s", appName, deploymentID)
	if srcRef == dstRef {
		return dstRef, nil
	}

	if err := r.cli.ImageTag(ctx, srcRef, dstRef); err != nil {
		return dstRef, fmt.Errorf("tag image: %w", err)
	}
	return dstRef, nil
}

func (r runtimeDockerOps) StopContainers(ctx context.Context, logger *slog.Logger, appName, ignoreDeploymentID string) ([]string, error) {
	return docker.StopContainers(ctx, r.cli, logger, appName, ignoreDeploymentID)
}

func (r runtimeDockerOps) RemoveContainers(ctx context.Context, logger *slog.Logger, appName, ignoreDeploymentID string) ([]string, error) {
	return docker.RemoveContainers(ctx, r.cli, logger, appName, ignoreDeploymentID)
}

func (r runtimeDockerOps) EnsureVolumes(ctx context.Context, logger *slog.Logger, appName string, volumes []string) error {
	return docker.EnsureVolumes(ctx, r.cli, logger, appName, volumes)
}

func (r runtimeDockerOps) RunContainer(ctx context.Context, deploymentID, imageRef string, targetConfig config.TargetConfig) ([]docker.ContainerRunResult, error) {
	return docker.RunContainer(ctx, r.cli, deploymentID, imageRef, targetConfig)
}

func (r runtimeDockerOps) RemoveImages(ctx context.Context, logger *slog.Logger, appName, ignoreDeploymentID string, deploymentsToKeep int) error {
	return docker.RemoveImages(ctx, r.cli, logger, appName, ignoreDeploymentID, deploymentsToKeep)
}

type runtimeStoreFactory struct{}

func (runtimeStoreFactory) Open() (deploymentStore, error) {
	return storage.New()
}

func DeployApp(ctx context.Context, cli *client.Client, deploymentID string, targetConfig config.TargetConfig, rawDeployConfig config.DeployConfig, logger *slog.Logger) error {
	return deployAppWithDeps(ctx, deploymentID, targetConfig, rawDeployConfig, logger, runtimeDockerOps{cli: cli}, runtimeStoreFactory{})
}

func deployAppWithDeps(
	ctx context.Context,
	deploymentID string,
	targetConfig config.TargetConfig,
	rawDeployConfig config.DeployConfig,
	logger *slog.Logger,
	dockerOps deployDockerOps,
	storeFactory deploymentStoreFactory,
) error {
	imageRef := targetConfig.Image.ImageRef()

	err := dockerOps.EnsureImageUpToDate(ctx, logger, *targetConfig.Image)
	if err != nil {
		return err
	}

	newImageRef := imageRef
	if targetConfig.Image == nil || targetConfig.Image.History == nil ||
		targetConfig.Image.History.Strategy != config.HistoryStrategyNone {
		var err error
		newImageRef, err = dockerOps.TagImage(ctx, imageRef, targetConfig.Name, deploymentID)
		if err != nil {
			return fmt.Errorf("failed to tag image: %w", err)
		}
	}

	if targetConfig.DeploymentStrategy == config.DeploymentStrategyReplace {
		_, err := dockerOps.StopContainers(ctx, logger, targetConfig.Name, "")
		if err != nil {
			return fmt.Errorf("failed to stop containers before starting new deployment: %w", err)
		}
		_, err = dockerOps.RemoveContainers(ctx, logger, targetConfig.Name, "")
		if err != nil {
			return fmt.Errorf("failed to remove containers before starting new deployment: %w", err)
		}
	}

	if len(targetConfig.Volumes) > 0 {
		if err := dockerOps.EnsureVolumes(ctx, logger, targetConfig.Name, targetConfig.Volumes); err != nil {
			return fmt.Errorf("failed to ensure volumes: %w", err)
		}
	}

	runResult, err := dockerOps.RunContainer(ctx, deploymentID, newImageRef, targetConfig)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("container startup timed out: %w", err)
		} else if errors.Is(err, context.Canceled) {
			logger.Warn("Deployment canceled", "error", err)
			if ctx.Err() != nil {
				return fmt.Errorf("deployment canceled: %w", ctx.Err())
			}
			return fmt.Errorf("container creation canceled: %w", err)
		}
		return err
	}

	if len(runResult) == 0 {
		return fmt.Errorf("no containers started, check logs for details")
	} else if len(runResult) == 1 {
		logger.Info(fmt.Sprintf("Container started for %s", targetConfig.Name), "containerID", runResult[0].ID, "deploymentID", deploymentID)
	} else {
		logger.Info(fmt.Sprintf("Containers started for %s (%d replicas)", targetConfig.Name, len(runResult)), "count", len(runResult), "deploymentID", deploymentID)
	}

	handleImageHistory(ctx, rawDeployConfig, deploymentID, newImageRef, logger, dockerOps, storeFactory)

	return nil
}

func handleImageHistory(
	ctx context.Context,
	rawDeployConfig config.DeployConfig,
	deploymentID,
	newImageRef string,
	logger *slog.Logger,
	dockerOps deployDockerOps,
	storeFactory deploymentStoreFactory,
) {
	image := rawDeployConfig.Image

	if image == nil {
		logger.Debug("No image configuration found, skipping history management")
		return
	}

	strategy := config.HistoryStrategyLocal
	if image.History != nil {
		strategy = image.History.Strategy
	}

	switch strategy {
	case config.HistoryStrategyNone:
		logger.Debug("History disabled, skipping cleanup and history storage")

	case config.HistoryStrategyLocal:
		if err := writeDeployConfigHistory(rawDeployConfig, deploymentID, newImageRef, storeFactory); err != nil {
			logger.Warn("Failed to write deploy config history", "error", err)
		} else {
			logger.Debug("App configuration saved to history")
		}

		if err := dockerOps.RemoveImages(ctx, logger, rawDeployConfig.Name, deploymentID, *rawDeployConfig.Image.History.Count); err != nil {
			logger.Warn("Failed to clean up old images", "error", err)
		} else {
			logger.Debug(fmt.Sprintf("Old images cleaned up, keeping %d recent images locally", *rawDeployConfig.Image.History.Count))
		}

	case config.HistoryStrategyRegistry:
		if err := writeDeployConfigHistory(rawDeployConfig, deploymentID, newImageRef, storeFactory); err != nil {
			logger.Warn("Failed to write deploy config history", "error", err)
		} else {
			logger.Debug("App configuration saved to history")
		}

		if err := dockerOps.RemoveImages(ctx, logger, rawDeployConfig.Name, deploymentID, 1); err != nil {
			logger.Warn("Failed to clean up old images", "error", err)
		} else {
			logger.Debug("Old images cleaned up, registry strategy - keeping only current image locally")
		}

	default:
		logger.Warn("Unknown history strategy, skipping history management", "strategy", rawDeployConfig.Image.History.Strategy)
	}
}

// writeDeployConfigHistory writes the given deployConfig to the db. It will save the newImageRef as a json repsentation of the Image struct to use for rollbacks
func writeDeployConfigHistory(rawDeployConfig config.DeployConfig, deploymentID, newImageRef string, storeFactory deploymentStoreFactory) error {
	if rawDeployConfig.Image.History == nil {
		return fmt.Errorf("image.history must be set")
	}

	if rawDeployConfig.Image.History.Strategy != config.HistoryStrategyNone && rawDeployConfig.Image.History.Count == nil {
		return fmt.Errorf("image.history.count is required for %s strategy", rawDeployConfig.Image.History.Strategy)
	}

	store, err := storeFactory.Open()
	if err != nil {
		return err
	}
	defer store.Close()

	rawDeployConfigJSON, err := json.Marshal(rawDeployConfig)
	if err != nil {
		return fmt.Errorf("failed to convert target config to JSON: %w", err)
	}

	deployedImage := rawDeployConfig.Image
	if parts := strings.SplitN(newImageRef, ":", 2); len(parts) == 2 {
		deployedImage.Repository = parts[0]
		deployedImage.Tag = parts[1]
	}

	deployedImageJSON, err := json.Marshal(deployedImage)
	if err != nil {
		return fmt.Errorf("failed to convert deployed image to JSON: %w", err)
	}

	deployment := storage.Deployment{
		ID:              deploymentID,
		AppName:         rawDeployConfig.Name,
		RawDeployConfig: rawDeployConfigJSON,
		DeployedImage:   deployedImageJSON,
	}

	if err := store.SaveDeployment(deployment); err != nil {
		return fmt.Errorf("failed to save deployment to database: %w", err)
	}

	if err := store.PruneOldDeployments(rawDeployConfig.Name, *rawDeployConfig.Image.History.Count); err != nil {
		return fmt.Errorf("failed to prune old deployments: %w", err)
	}

	return nil
}
