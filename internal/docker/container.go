package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/healthcheck"
	"github.com/haloydev/haloy/internal/helpers"
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
		Port:            targetConfig.Port,
		HealthCheckPath: targetConfig.HealthCheckPath,
		Domains:         targetConfig.Domains,
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

		var containerName string
		if targetConfig.NamingStrategy == config.NamingStrategyStatic {
			containerName = targetConfig.Name
		} else {
			containerName = fmt.Sprintf("%s-%s", targetConfig.Name, deploymentID)
		}

		if *targetConfig.Replicas > 1 {
			containerName += fmt.Sprintf("-r%d", i+1)
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
		stoppedIDs, err = stopContainersSequential(stopCtx, cli, logger, containersToStop)
	} else {
		logger.Info(fmt.Sprintf("Stopping %d containers. This might take a moment...", len(containersToStop)))
		stoppedIDs, err = stopContainersConcurrent(stopCtx, cli, logger, containersToStop)
	}

	if err != nil {
		return stoppedIDs, err
	}

	// Verify all stopped containers have reached "exited" state
	for _, containerID := range stoppedIDs {
		if err := waitForContainerExited(ctx, cli, containerID, 10*time.Second); err != nil {
			return stoppedIDs, fmt.Errorf("failed to verify container stopped: %w", err)
		}
	}
	return stoppedIDs, nil
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
	stopOptions := container.StopOptions{Timeout: helpers.Ptr(20)}

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

// HealthCheckResult contains the result of a container health check.
type HealthCheckResult struct {
	IP  string // Container IP address on the haloy network (only set on success)
	Err error  // nil if healthy
}

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
			// If already removed, that's fine
			if client.IsErrNotFound(err) {
				continue
			}
			return removedIDs, fmt.Errorf("failed to remove container %s: %w", helpers.SafeIDPrefix(containerInfo.ID), err)
		}
		// Verify container is actually removed
		if err := verifyContainerRemoved(ctx, cli, containerInfo.ID, 10*time.Second); err != nil {
			return removedIDs, err
		}
		removedIDs = append(removedIDs, containerInfo.ID)
	}
	return removedIDs, nil
}

// HealthCheckContainer performs health checks on a container and returns its IP address on success.
// It accepts a pre-fetched container.InspectResponse but will re-inspect if needed.
// The function checks:
// 1. Container is running (and stable, not in restart loop)
// 2. Docker health status (if configured)
// 3. HTTP health check endpoint (if no Docker healthcheck)
// 4. Container has a valid IP on the haloy network
func HealthCheckContainer(ctx context.Context, cli *client.Client, logger *slog.Logger, containerID string, containerInfo container.InspectResponse) HealthCheckResult {
	// Re-inspect container to get fresh state (the passed containerInfo may be stale)
	freshInfo, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return HealthCheckResult{Err: fmt.Errorf("failed to inspect container: %w", err)}
	}
	containerInfo = freshInfo

	// Check if container is running
	if containerInfo.State == nil {
		return HealthCheckResult{Err: fmt.Errorf("container state is nil")}
	}

	// Check for restarting state - this indicates the container is crash-looping
	if containerInfo.State.Restarting {
		exitCode := containerInfo.State.ExitCode
		return HealthCheckResult{Err: fmt.Errorf("container is restarting (exit code: %d) - check container logs for details", exitCode)}
	}

	if !containerInfo.State.Running {
		exitCode := containerInfo.State.ExitCode
		return HealthCheckResult{Err: fmt.Errorf("container is not running (status: %s, exit code: %d)", containerInfo.State.Status, exitCode)}
	}

	// Get the container's IP address early - we need it for health checks and as the result
	targetIP, err := ContainerNetworkIP(containerInfo, constants.DockerNetwork)
	if err != nil {
		return HealthCheckResult{Err: fmt.Errorf("failed to get container IP address: %w", err)}
	}

	// Check Docker's built-in health status if available
	if containerInfo.State.Health != nil {
		if containerInfo.State.Health.Status == "healthy" {
			logger.Debug("Container is healthy according to Docker healthcheck", "container_id", helpers.SafeIDPrefix(containerID))
			return HealthCheckResult{IP: targetIP}
		}

		if containerInfo.State.Health.Status == "starting" {
			healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			var latestInfo container.InspectResponse
			for {
				latestInfo, err = cli.ContainerInspect(healthCtx, containerID)
				if err != nil {
					return HealthCheckResult{Err: fmt.Errorf("failed to re-inspect container: %w", err)}
				}

				if latestInfo.State.Health.Status != "starting" {
					containerInfo = latestInfo
					break
				}

				select {
				case <-healthCtx.Done():
					return HealthCheckResult{Err: fmt.Errorf("timed out waiting for container health check to complete")}
				case <-time.After(1 * time.Second):
				}
			}
		}

		switch containerInfo.State.Health.Status {
		case "healthy":
			logger.Debug("Container is healthy according to Docker healthcheck", "container_id", helpers.SafeIDPrefix(containerID))
			return HealthCheckResult{IP: targetIP}
		case "starting":
			logger.Info("Container is still starting, falling back to manual health check", "container_id", helpers.SafeIDPrefix(containerID))
		case "unhealthy":
			if len(containerInfo.State.Health.Log) > 0 {
				lastLog := containerInfo.State.Health.Log[len(containerInfo.State.Health.Log)-1]
				return HealthCheckResult{Err: fmt.Errorf("container is unhealthy: %s", lastLog.Output)}
			}
			return HealthCheckResult{Err: fmt.Errorf("container is unhealthy according to Docker healthcheck")}
		default:
			return HealthCheckResult{Err: fmt.Errorf("container health status unknown: %s", containerInfo.State.Health.Status)}
		}
	}

	// No Docker healthcheck configured, perform manual HTTP health check
	labels, err := config.ParseContainerLabels(containerInfo.Config.Labels)
	if err != nil {
		return HealthCheckResult{Err: fmt.Errorf("failed to parse container labels: %w", err)}
	}

	if labels.Port == "" {
		return HealthCheckResult{Err: fmt.Errorf("container has no port label set")}
	}

	if labels.HealthCheckPath == "" {
		return HealthCheckResult{Err: fmt.Errorf("container has no health check path set")}
	}

	// Use the unified healthcheck package for HTTP health checks
	target := healthcheck.Target{
		ID:              containerID,
		AppName:         labels.AppName,
		IP:              targetIP,
		Port:            labels.Port.String(),
		HealthCheckPath: labels.HealthCheckPath,
	}

	checker := healthcheck.NewHTTPChecker(5 * time.Second)
	retryConfig := healthcheck.DefaultRetryConfig()

	result := checker.CheckWithRetry(ctx, target, retryConfig, func(attempt int, backoff time.Duration) {
		logger.Info("Retrying health check...",
			"backoff", backoff,
			"attempt", attempt+1,
			"max_retries", retryConfig.MaxRetries+1)
	})

	if result.Healthy {
		return HealthCheckResult{IP: targetIP}
	}

	return HealthCheckResult{Err: result.Err}
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
	if appName != "" {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))
	} else {
		// Filter by presence of LabelAppName to identify Haloy-managed containers
		filterArgs.Add("label", config.LabelAppName)
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

// ExecInContainer executes a command in a running container and returns the output.
func ExecInContainer(ctx context.Context, cli *client.Client, containerID string, cmd []string) (stdout, stderr string, exitCode int, err error) {
	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}
	execID, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", "", 1, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", 1, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()
	// Read stdout and stderr using stdcopy to demultiplex the streams
	var stdoutBuf, stderrBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, resp.Reader)
	if err != nil {
		return "", "", 1, fmt.Errorf("failed to read exec output: %w", err)
	}
	// Get the exit code
	inspectResp, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return stdoutBuf.String(), stderrBuf.String(), 1, fmt.Errorf("failed to inspect exec: %w", err)
	}
	return stdoutBuf.String(), stderrBuf.String(), inspectResp.ExitCode, nil
}

type LogLine struct {
	ContainerID string `json:"containerId"`
	Line        string `json:"line"`
}

func StreamContainerLogs(ctx context.Context, cli *client.Client, containerID string, tail int) (<-chan LogLine, error) {
	if tail <= 0 {
		tail = 100
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       fmt.Sprintf("%d", tail),
		Timestamps: false,
	}

	reader, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, fmt.Errorf("failed to stream container logs: %w", err)
	}

	ch := make(chan LogLine, 100)

	go func() {
		defer close(ch)
		defer reader.Close()

		hdr := make([]byte, 8)
		for {
			_, err := io.ReadFull(reader, hdr)
			if err != nil {
				return
			}

			size := int(hdr[4])<<24 | int(hdr[5])<<16 | int(hdr[6])<<8 | int(hdr[7])
			if size == 0 {
				continue
			}

			payload := make([]byte, size)
			_, err = io.ReadFull(reader, payload)
			if err != nil {
				return
			}

			line := string(bytes.TrimRight(payload, "\n"))
			if line == "" {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case ch <- LogLine{ContainerID: containerID, Line: line}:
			}
		}
	}()

	return ch, nil
}

// GetContainerLogs retrieves the last N lines of logs from a container.
// This works even for stopped containers, making it useful for debugging failed deployments.
func GetContainerLogs(ctx context.Context, cli *client.Client, containerID string, tailLines int) (string, error) {
	if tailLines <= 0 {
		tailLines = 100
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tailLines),
		Timestamps: false,
	}

	reader, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer reader.Close()

	// Docker multiplexes stdout and stderr in the log stream
	// Use stdcopy to demultiplex them into a single buffer
	var outputBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&outputBuf, &outputBuf, reader)
	if err != nil {
		// If stdcopy fails, the container might be using TTY mode
		// In that case, read directly from the reader
		reader, err = cli.ContainerLogs(ctx, containerID, options)
		if err != nil {
			return "", fmt.Errorf("failed to get container logs (retry): %w", err)
		}
		defer reader.Close()
		_, err = io.Copy(&outputBuf, reader)
		if err != nil {
			return "", fmt.Errorf("failed to read container logs: %w", err)
		}
	}

	return outputBuf.String(), nil
}

// waitForContainerExited polls until the container reaches "exited" state or timeout.
// Returns nil if container is exited, error if timeout or inspection fails.
func waitForContainerExited(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastStatus string = "unknown"
	// Check immediately before starting ticker
	containerInfo, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to inspect container %s: %w", helpers.SafeIDPrefix(containerID), err)
	}
	if containerInfo.State != nil {
		lastStatus = containerInfo.State.Status
		if lastStatus == "exited" {
			return nil
		}
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for container %s to exit (current status: %s)",
				helpers.SafeIDPrefix(containerID), lastStatus)
		case <-ticker.C:
			containerInfo, err = cli.ContainerInspect(ctx, containerID)
			if err != nil {
				if client.IsErrNotFound(err) {
					return nil
				}
				continue
			}
			if containerInfo.State != nil {
				lastStatus = containerInfo.State.Status
				if lastStatus == "exited" {
					return nil
				}
			}
		}
	}
}

// verifyContainerRemoved polls until the container no longer exists or timeout.
// Returns nil if container is removed, error if timeout or unexpected error.
func verifyContainerRemoved(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Check immediately before starting ticker
	_, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to inspect container %s: %w", helpers.SafeIDPrefix(containerID), err)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for container %s to be removed",
				helpers.SafeIDPrefix(containerID))
		case <-ticker.C:
			_, err := cli.ContainerInspect(ctx, containerID)
			if err != nil {
				if client.IsErrNotFound(err) {
					return nil
				}
				// Transient error, continue polling
				continue
			}
			// Container still exists, keep waiting
		}
	}
}
