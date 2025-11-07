package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type ContainerRunResult struct {
	ID           string
	DeploymentID string
	ReplicaID    int
}

func RunContainer(ctx context.Context, cli *client.Client, deploymentID, imageRef string, targetConfig config.TargetConfig) ([]ContainerRunResult, error) {
	result := make([]ContainerRunResult, 0, *targetConfig.Replicas)

	if err := checkImagePlatformCompatibility(ctx, cli, imageRef); err != nil {
		return result, err
	}
	cl := config.ContainerLabels{
		AppName:         targetConfig.Name,
		DeploymentID:    deploymentID,
		ACMEEmail:       targetConfig.ACMEEmail,
		Port:            targetConfig.Port,
		HealthCheckPath: targetConfig.HealthCheckPath,
		Domains:         targetConfig.Domains,
		Role:            config.AppLabelRole,
	}
	labels := cl.ToLabels()

	var envVars []string

	for _, envVar := range targetConfig.Env {
		envVars = append(envVars, fmt.Sprintf("%s=%s", envVar.Name, envVar.Value))
	}

	network := container.NetworkMode(constants.DockerNetwork)
	if targetConfig.Network != "" {
		network = container.NetworkMode(targetConfig.Network)
	}
	hostConfig := &container.HostConfig{
		NetworkMode:   network,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Binds:         targetConfig.Volumes,
	}

	for i := range make([]struct{}, *targetConfig.Replicas) {
		envVars := append(envVars, fmt.Sprintf("%s=%d", constants.EnvVarReplicaID, i+1))
		containerConfig := &container.Config{
			Image:  imageRef,
			Labels: labels,
			Env:    envVars,
		}
		containerName := fmt.Sprintf("%s-haloy-%s", targetConfig.Name, deploymentID)
		if *targetConfig.Replicas > 1 {
			containerName += fmt.Sprintf("-replica-%d", i+1)
		}

		createResponse, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
		if err != nil {
			return result, fmt.Errorf("failed to create container: %w", err)
		}

		defer func(containerID string) {
			if err != nil && containerID != "" {
				removeErr := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
				if removeErr != nil {
					fmt.Printf("Failed to clean up container after error: %v\n", removeErr)
				}
			}
		}(createResponse.ID)

		if err := cli.ContainerStart(ctx, createResponse.ID, container.StartOptions{}); err != nil {
			return result, fmt.Errorf("failed to start container: %w", err)
		}

		result = append(result, ContainerRunResult{
			ID:           createResponse.ID,
			DeploymentID: deploymentID,
			ReplicaID:    i + 1,
		})

	}

	return result, nil
}

func StopContainers(ctx context.Context, cli *client.Client, logger *slog.Logger, appName, ignoreDeploymentID string) (stoppedIDs []string, err error) {
	containerList, err := GetAppContainers(ctx, cli, true, appName)
	if err != nil {
		return stoppedIDs, err
	}

	var containersToStop []container.Summary
	for _, containerInfo := range containerList {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]
		if deploymentID != ignoreDeploymentID {
			containersToStop = append(containersToStop, containerInfo)
		}
	}

	if len(containersToStop) == 0 {
		return stoppedIDs, nil
	}

	stopCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if len(containersToStop) <= 3 {
		return stopContainersSequential(stopCtx, cli, logger, containersToStop)
	}

	logger.Info(fmt.Sprintf("Stopping %d containers. This might take a moment...", len(containersToStop)))
	return stopContainersConcurrent(stopCtx, cli, logger, containersToStop)
}

func stopContainersSequential(ctx context.Context, cli *client.Client, logger *slog.Logger, containers []container.Summary) ([]string, error) {
	var stoppedIDs []string
	var errors []error

	for _, containerInfo := range containers {
		if err := stopSingleContainer(ctx, cli, logger, containerInfo.ID); err != nil {
			errors = append(errors, err)
		} else {
			stoppedIDs = append(stoppedIDs, containerInfo.ID)
		}
	}

	var err error
	if len(errors) > 0 {
		err = fmt.Errorf("failed to stop %d out of %d containers", len(errors), len(containers))
	}

	return stoppedIDs, err
}

func stopContainersConcurrent(ctx context.Context, cli *client.Client, logger *slog.Logger, containers []container.Summary) ([]string, error) {
	type result struct {
		containerID string
		error       error
	}

	resultChan := make(chan result, len(containers))
	semaphore := make(chan struct{}, 3)

	for _, containerInfo := range containers {
		go func(container container.Summary) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			err := stopSingleContainer(ctx, cli, logger, container.ID)
			resultChan <- result{containerID: container.ID, error: err}
		}(containerInfo)
	}

	var stoppedIDs []string
	var errors []error

	for range len(containers) {
		res := <-resultChan
		if res.error != nil {
			errors = append(errors, res.error)
		} else {
			stoppedIDs = append(stoppedIDs, res.containerID)
		}
	}

	var err error
	if len(errors) > 0 {
		err = fmt.Errorf("failed to stop %d out of %d containers", len(errors), len(containers))
	}

	return stoppedIDs, err
}

func stopSingleContainer(ctx context.Context, cli *client.Client, logger *slog.Logger, containerID string) error {
	timeout := 20
	stopOptions := container.StopOptions{Timeout: &timeout}

	err := cli.ContainerStop(ctx, containerID, stopOptions)
	if err == nil {
		return nil
	}

	logger.Warn("Graceful stop failed, attempting force kill", "container_id", helpers.SafeIDPrefix(containerID), "error", err)

	killErr := cli.ContainerKill(ctx, containerID, "SIGKILL")
	if killErr != nil {
		return fmt.Errorf("both stop and kill failed - stop: %v, kill: %v", err, killErr)
	}

	return nil
}

type RemoveContainersResult struct {
	ID           string
	DeploymentID string
}

// RemoveContainers attempts to remove old containers for a given app and ignoring a specific deployment.
func RemoveContainers(ctx context.Context, cli *client.Client, logger *slog.Logger, appName, ignoreDeploymentID string) (removedIDs []string, err error) {
	containerList, err := GetAppContainers(ctx, cli, true, appName)
	if err != nil {
		return removedIDs, err
	}

	for _, containerInfo := range containerList {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]
		if deploymentID == ignoreDeploymentID {
			continue
		}

		err := cli.ContainerRemove(ctx, containerInfo.ID, container.RemoveOptions{Force: true})
		if err != nil {
			logger.Error("Error removing container %s: %v\n", helpers.SafeIDPrefix(containerInfo.ID), err)
		} else {
			removedIDs = append(removedIDs, containerInfo.ID)
		}
	}

	return removedIDs, nil
}

func HealthCheckContainer(ctx context.Context, cli *client.Client, logger *slog.Logger, containerID string, initialWaitTime ...time.Duration) error {
	// Check if container is running - wait up to 30 seconds for it to start
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var containerInfo container.InspectResponse
	var err error

	for {
		containerInfo, err = cli.ContainerInspect(startCtx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container %s: %w", helpers.SafeIDPrefix(containerID), err)
		}

		if containerInfo.State.Running {
			break
		}

		select {
		case <-startCtx.Done():
			return fmt.Errorf("timed out waiting for container %s to start", helpers.SafeIDPrefix(containerID))
		case <-time.After(500 * time.Millisecond):
		}
	}

	if len(initialWaitTime) > 0 && initialWaitTime[0] > 0 {
		waitTime := initialWaitTime[0]

		waitTimer := time.NewTimer(waitTime)
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled during initial wait period")
		case <-waitTimer.C:
		}
	}

	if containerInfo.State.Health != nil {
		if containerInfo.State.Health.Status == "healthy" {
			return nil
		}

		if containerInfo.State.Health.Status == "starting" {
			healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			for {
				containerInfo, err = cli.ContainerInspect(healthCtx, containerID)
				if err != nil {
					return fmt.Errorf("failed to re-inspect container: %w", err)
				}

				if containerInfo.State.Health.Status != "starting" {
					break
				}

				select {
				case <-healthCtx.Done():
					return fmt.Errorf("timed out waiting for container health check to complete")
				case <-time.After(1 * time.Second):
				}
			}
		}

		switch containerInfo.State.Health.Status {
		case "healthy":
			logger.Debug("Container is healthy according to Docker healthcheck", "container_id", helpers.SafeIDPrefix(containerID))
			return nil
		case "starting":
			logger.Info("Container is still starting, falling back to manual health check", "container_id", helpers.SafeIDPrefix(containerID))
		case "unhealthy":
			if len(containerInfo.State.Health.Log) > 0 {
				lastLog := containerInfo.State.Health.Log[len(containerInfo.State.Health.Log)-1]
				return fmt.Errorf("container %s is unhealthy: %s", helpers.SafeIDPrefix(containerID), lastLog.Output)
			}
			return fmt.Errorf("container %s is unhealthy according to Docker healthcheck", helpers.SafeIDPrefix(containerID))
		default:
			return fmt.Errorf("container %s health status unknown: %s", helpers.SafeIDPrefix(containerID), containerInfo.State.Health.Status)
		}
	}

	labels, err := config.ParseContainerLabels(containerInfo.Config.Labels)
	if err != nil {
		return fmt.Errorf("failed to parse container labels: %w", err)
	}

	if labels.Port == "" {
		return fmt.Errorf("container %s has no port label set", helpers.SafeIDPrefix(containerID))
	}

	if labels.HealthCheckPath == "" {
		return fmt.Errorf("container %s has no health check path set", helpers.SafeIDPrefix(containerID))
	}

	targetIP, err := ContainerNetworkIP(containerInfo, constants.DockerNetwork)
	if err != nil {
		return fmt.Errorf("failed to get container IP address: %w", err)
	}

	healthCheckURL := fmt.Sprintf("http://%s:%s%s", targetIP, labels.Port, labels.HealthCheckPath)
	maxRetries := 5
	backoff := 500 * time.Millisecond

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			logger.Info("Retrying health check...", "backoff", backoff, "attempt", retry+1, "max_retries", maxRetries)
			time.Sleep(backoff)
			backoff *= 2
		}

		req, err := http.NewRequestWithContext(ctx, "GET", healthCheckURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Warn("Health check attempt failed", "error", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logger.Warn("Health check returned error status", "status_code", resp.StatusCode, "response", string(bodyBytes))
	}

	return fmt.Errorf("container %s failed health check after %d attempts", helpers.SafeIDPrefix(containerID), maxRetries)
}

// GetAppContainers returns a slice of container summaries filtered by labels.
//
// Parameters:
//   - ctx: the context for the Docker API requests.
//   - cli: the Docker client used to interact with the Docker daemon.
//   - listAll: if true, the function returns all containers including stopped ones;
//     if false, only running containers are returned.
//   - appName: if not empty, only containers associated with the given app name are returned.
//
// Returns:
//   - A slice of container summaries.
//   - An error if something went wrong during the container listing.
func GetAppContainers(ctx context.Context, cli *client.Client, listAll bool, appName string) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	if appName != "" {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))
	}
	containerList, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     listAll,
	})
	if err != nil {
		if appName != "" {
			return nil, fmt.Errorf("failed to list containers for app %s: %w", appName, err)
		} else {
			return nil, fmt.Errorf("failed to list containers: %w", err)
		}
	}

	return containerList, nil
}

// ContainerNetworkInfo extracts the container's IP address
func ContainerNetworkIP(containerInfo container.InspectResponse, networkName string) (string, error) {
	if containerInfo.State == nil {
		return "", fmt.Errorf("container state is nil")
	}

	if !containerInfo.State.Running {
		exitCode := 0
		if containerInfo.State.ExitCode != 0 {
			exitCode = containerInfo.State.ExitCode
		}
		return "", fmt.Errorf("container is not running (status: %s, exit code: %d)", containerInfo.State.Status, exitCode)
	}

	if _, exists := containerInfo.NetworkSettings.Networks[networkName]; !exists {
		var availableNetworks []string
		for netName := range containerInfo.NetworkSettings.Networks {
			availableNetworks = append(availableNetworks, netName)
		}
		return "", fmt.Errorf("container not connected to network '%s'. Container is using: %v", networkName, availableNetworks)
	}

	ipAddress := containerInfo.NetworkSettings.Networks[networkName].IPAddress
	if ipAddress == "" {
		return "", fmt.Errorf("container has no IP address on network '%s'", networkName)
	}

	return ipAddress, nil
}

// checkImagePlatformCompatibility verifies the image platform matches the host
func checkImagePlatformCompatibility(ctx context.Context, cli *client.Client, imageRef string) error {
	imageInspect, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}

	hostInfo, err := cli.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get host info: %w", err)
	}

	imagePlatform := imageInspect.Architecture
	hostPlatform := hostInfo.Architecture

	platformMap := map[string]string{
		"x86_64":  "amd64",
		"aarch64": "arm64",
		"armv7l":  "arm",
	}

	if normalized, exists := platformMap[imagePlatform]; exists {
		imagePlatform = normalized
	}
	if normalized, exists := platformMap[hostPlatform]; exists {
		hostPlatform = normalized
	}

	if imagePlatform != hostPlatform {
		return fmt.Errorf(
			"image built for %s but host is %s. "+
				"Rebuild the image for the correct platform or use docker buildx with --platform flag",
			imagePlatform, hostPlatform,
		)
	}

	return nil
}
