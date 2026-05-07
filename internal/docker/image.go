package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
)

func getRegistryAuthString(imageConfig *config.Image) (string, error) {
	auth := imageConfig.RegistryAuth
	if auth == nil {
		return "", nil
	}
	server := imageConfig.GetRegistryServer()
	authConfig := registry.AuthConfig{
		Username:      auth.Username.Value,
		Password:      auth.Password.Value,
		ServerAddress: server,
	}
	authStr, err := registry.EncodeAuthConfig(authConfig)
	if err != nil {
		return "", fmt.Errorf("failed to encode auth config for %s: %w", server, err)
	}
	return authStr, nil
}

func isDockerHubServer(server string) bool {
	return config.NormalizeRegistryServer(server) == "docker.io"
}

func isDockerHubImage(imageConfig config.Image) bool {
	return isDockerHubServer(imageConfig.GetRegistryServer())
}

func isDockerHubPullRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "increase-rate-limit") ||
		(strings.Contains(message, "pull rate limit") && strings.Contains(message, "you have reached"))
}

func shouldWarnUnauthenticatedDockerHubPull(imageConfig config.Image, registryAuth string) bool {
	return registryAuth == "" && isDockerHubImage(imageConfig)
}

func normalizedPullRef(imageConfig config.Image) string {
	imageRef := imageConfig.ImageRef()
	server := config.NormalizeRegistryServer(imageConfig.GetRegistryServer())
	if server != "docker.io" {
		return imageRef
	}

	repository := strings.TrimSpace(imageConfig.Repository)
	if strings.Contains(repository, "/") {
		first, _, _ := strings.Cut(repository, "/")
		if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
			return imageRef
		}
		return "docker.io/" + imageRef
	}
	return "docker.io/library/" + imageRef
}

func registryAuthMode(registryAuth string) string {
	if registryAuth == "" {
		return "none"
	}
	return "configured"
}

func dockerHubPullRateLimitHint(imageConfig config.Image) string {
	if imageConfig.RegistryAuth == nil {
		return "Hint: Docker Hub rate limit reached. " +
			"Haloy pulled this Docker Hub image without registry credentials on the deployment server. " +
			"Run 'haloy server registry login <server-url> docker.io --username <user> --password-stdin', " +
			"add Docker Hub credentials to image.registry for this target, use a registry mirror/cache, " +
			"or wait for Docker Hub's pull limit window to reset. " +
			"A local docker login is not sent to remote deployments."
	}

	return "Hint: Docker Hub rate limit reached while using configured registry credentials. " +
		"Check whether the account has quota available, use a paid Docker Hub account or registry mirror/cache, " +
		"or wait for Docker Hub's pull limit window to reset."
}

func formatImagePullError(imageRef string, imageConfig config.Image, err error) error {
	if isDockerHubImage(imageConfig) && isDockerHubPullRateLimitError(err) {
		return fmt.Errorf("failed to pull %s: %w\n%s", imageRef, err, dockerHubPullRateLimitHint(imageConfig))
	}

	if !strings.ContainsAny(imageRef, "/.") {
		return fmt.Errorf("failed to pull %s: %w\nHint: if you intended to build this image locally, remove the 'image' field from your config or set 'build: true'.", imageRef, err)
	}
	return fmt.Errorf("failed to pull %s: %w", imageRef, err)
}

func EnsureImageUpToDate(ctx context.Context, cli *client.Client, logger *slog.Logger, imageConfig config.Image) error {
	imageRef := imageConfig.ImageRef()
	pullPolicy := imageConfig.EffectivePullPolicy()

	local, err := cli.ImageInspect(ctx, imageRef)
	localExists := (err == nil)

	// If BuildConfig. is true the server should have a local copy that was uploaded.
	if imageConfig.BuildConfig != nil && imageConfig.BuildConfig.Push == config.BuildPushOptionServer {
		if !localExists {
			return fmt.Errorf("uploaded image '%s' not found", imageRef)
		}
		logger.Debug("Using local image", "image", imageRef)
		return nil
	}

	if localExists && pullPolicy == config.PullPolicyIfMissing {
		logger.Info("Using local image", "image", normalizedPullRef(imageConfig), "pull_policy", pullPolicy)
		return nil
	}

	if pullPolicy == config.PullPolicyNever {
		if localExists {
			logger.Info("Using local image", "image", normalizedPullRef(imageConfig), "pull_policy", pullPolicy)
			return nil
		}
		return fmt.Errorf("image '%s' not found locally and image.pull_policy is 'never'", imageRef)
	}

	registryAuth, err := getRegistryAuthString(&imageConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve registry auth for image %s: %w", imageRef, err)
	}

	if localExists {
		remote, err := cli.DistributionInspect(ctx, imageRef, registryAuth)
		if err != nil {
			if isDockerHubImage(imageConfig) && isDockerHubPullRateLimitError(err) {
				logger.Warn("Failed to check Docker Hub for image updates due to rate limits; using local image", "image", normalizedPullRef(imageConfig), "error", err)
			} else {
				logger.Debug("Failed to check remote registry, using local image", "image", imageRef, "error", err)
			}
			return nil
		}

		remoteDigest := remote.Descriptor.Digest.String()
		if local.RepoDigests != nil {
			for _, rd := range local.RepoDigests {
				if strings.HasSuffix(rd, "@"+remoteDigest) {
					logger.Debug("Registry image is up to date", "image", imageRef)
					return nil // Local matches remote - use local
				}
			}
		}
		logger.Debug("Local image outdated, pulling from registry", "image", imageRef)
	}

	// If we reach here, either the image doesn't exist locally or the remote digest doesn't match
	logger.Info("Pulling image", "image", normalizedPullRef(imageConfig), "registry", config.NormalizeRegistryServer(imageConfig.GetRegistryServer()), "auth", registryAuthMode(registryAuth), "pull_policy", pullPolicy)
	if shouldWarnUnauthenticatedDockerHubPull(imageConfig, registryAuth) {
		logger.Warn("Pulling Docker Hub image without registry credentials; this may hit Docker Hub anonymous pull limits", "image", imageRef)
	}

	r, err := cli.ImagePull(ctx, imageRef, image.PullOptions{
		RegistryAuth: registryAuth,
	})
	if err != nil {
		return formatImagePullError(imageRef, imageConfig, err)
	}
	defer r.Close()
	// drain stream
	if _, err := io.Copy(io.Discard, r); err != nil {
		return fmt.Errorf("error reading pull response: %w", err)
	}
	logger.Debug("Successfully pulled image", "image", imageRef)
	return nil
}

// PruneImages removes dangling (unused) Docker images and returns the amount of space reclaimed.
func PruneImages(ctx context.Context, cli *client.Client, logger *slog.Logger) (uint64, error) {
	report, err := cli.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return 0, fmt.Errorf("failed to prune images: %w", err)
	}
	if len(report.ImagesDeleted) > 0 {
		logger.Info("Pruned images", "count", len(report.ImagesDeleted), "bytes_reclaimed", report.SpaceReclaimed)
	}
	return report.SpaceReclaimed, nil
}

// RemoveImages removes extra image tags for a given app, keeping the requested number of deployments in total.
// When ignoreDeploymentID is set, that deployment is counted as one of the kept deployments and excluded from removal candidates.
// Running containers reference the image by digest; if an image is in use we allow removal of duplicate tags as long as at least one tag is preserved.
type removableImageTag struct {
	Tag          string
	DeploymentID string
	ImageID      string
}

type ImagePruneTag struct {
	Tag          string
	DeploymentID string
	ImageID      string
}

type ImagePrunePlan struct {
	AppName              string
	Keep                 int
	RunningDeploymentIDs []string
	Tags                 []ImagePruneTag
}

func selectImageTagsToRemove(candidates []removableImageTag, inUseImageIDs map[string]struct{}, deploymentsToKeep int, ignoreDeploymentID string) []removableImageTag {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].DeploymentID > candidates[j].DeploymentID
	})

	keepFromCandidates := deploymentsToKeep
	if ignoreDeploymentID != "" && keepFromCandidates > 0 {
		keepFromCandidates--
	}

	keepTags := make(map[string]struct{})
	keepImageIDs := make(map[string]struct{})
	for i, cand := range candidates {
		if i < keepFromCandidates {
			keepTags[cand.Tag] = struct{}{}
			keepImageIDs[cand.ImageID] = struct{}{}
		}
	}

	var removals []removableImageTag
	for _, cand := range candidates {
		if _, ok := keepTags[cand.Tag]; ok {
			continue
		}

		_, inUse := inUseImageIDs[cand.ImageID]
		_, idInKeep := keepImageIDs[cand.ImageID]
		if inUse && !idInKeep {
			continue
		}

		removals = append(removals, cand)
	}

	return removals
}

func runningDeploymentIDs(containers []container.Summary) []string {
	seen := make(map[string]struct{})
	var deploymentIDs []string
	for _, containerInfo := range containers {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]
		if deploymentID == "" {
			continue
		}
		if _, exists := seen[deploymentID]; exists {
			continue
		}
		seen[deploymentID] = struct{}{}
		deploymentIDs = append(deploymentIDs, deploymentID)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(deploymentIDs)))
	return deploymentIDs
}

func planImagePrune(
	candidates []removableImageTag,
	inUseImageIDs map[string]struct{},
	appName,
	ignoreDeploymentID string,
	deploymentsToKeep int,
	runningIDs []string,
) ImagePrunePlan {
	removals := selectImageTagsToRemove(candidates, inUseImageIDs, deploymentsToKeep, ignoreDeploymentID)
	sort.Slice(removals, func(i, j int) bool {
		return removals[i].DeploymentID > removals[j].DeploymentID
	})

	plan := ImagePrunePlan{
		AppName:              appName,
		Keep:                 deploymentsToKeep,
		RunningDeploymentIDs: runningIDs,
		Tags:                 make([]ImagePruneTag, 0, len(removals)),
	}

	for _, removal := range removals {
		plan.Tags = append(plan.Tags, ImagePruneTag{
			Tag:          removal.Tag,
			DeploymentID: removal.DeploymentID,
			ImageID:      removal.ImageID,
		})
	}

	return plan
}

func PlanImagePrune(ctx context.Context, cli *client.Client, appName, ignoreDeploymentID string, deploymentsToKeep int) (ImagePrunePlan, error) {
	if deploymentsToKeep < 0 {
		return ImagePrunePlan{}, fmt.Errorf("deployments to keep must be at least 0")
	}

	// List all images for the app that match the format appName:<deploymentID>.
	images, err := cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", appName+":*")),
	})
	if err != nil {
		return ImagePrunePlan{}, fmt.Errorf("failed to list images for %s: %w", appName, err)
	}

	// Get a list of running containers for the app to determine which image digests are currently in use.
	containerList, err := GetAppContainers(ctx, cli, false, appName)
	if err != nil {
		return ImagePrunePlan{}, err
	}
	// Build a set of imageIDs that are in use.
	inUseImageIDs := make(map[string]struct{})
	for _, container := range containerList {
		if container.ImageID != "" {
			inUseImageIDs[container.ImageID] = struct{}{}
		}
	}

	// Build a candidate list of removable image tags.
	// Only consider tags that are not ":latest" and that start with the appName prefix.
	var candidates []removableImageTag
	for _, img := range images {
		for _, tag := range img.RepoTags {
			// Skip the "latest" and ignoreDeploymentID tag and any tag not matching the expected format.
			if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, ":"+ignoreDeploymentID) || !strings.HasPrefix(tag, appName+":") {
				continue
			}
			// Expected tag format: "appName:deploymentID", e.g. "test-app:20250615214304"
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) != 2 {
				// Unexpected tag format, skip this tag.
				continue
			}
			deploymentID := parts[1]
			candidates = append(candidates, removableImageTag{
				Tag:          tag,
				DeploymentID: deploymentID,
				ImageID:      img.ID,
			})
		}
	}

	return planImagePrune(
		candidates,
		inUseImageIDs,
		appName,
		ignoreDeploymentID,
		deploymentsToKeep,
		runningDeploymentIDs(containerList),
	), nil
}

func ExecuteImagePrunePlan(ctx context.Context, cli *client.Client, logger *slog.Logger, plan ImagePrunePlan) error {
	var errs []error
	for _, cand := range plan.Tags {
		if _, err := cli.ImageRemove(ctx, cand.Tag, image.RemoveOptions{Force: true, PruneChildren: false}); err != nil {
			logger.Error("Failed to remove image tag", "tag", cand.Tag, "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", cand.Tag, err))
		} else {
			logger.Debug("Removed image tag", "tag", cand.Tag)
		}
	}

	return errors.Join(errs...)
}

func RemoveImages(ctx context.Context, cli *client.Client, logger *slog.Logger, appName, ignoreDeploymentID string, deploymentsToKeep int) error {
	plan, err := PlanImagePrune(ctx, cli, appName, ignoreDeploymentID, deploymentsToKeep)
	if err != nil {
		return err
	}

	return ExecuteImagePrunePlan(ctx, cli, logger, plan)
}

func LoadImageFromTar(ctx context.Context, cli *client.Client, tarPath string) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("failed to open tar file: %w", err)
	}
	defer file.Close()

	response, err := cli.ImageLoad(ctx, file)
	if err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}
	defer response.Body.Close()

	// Read and parse the JSON response
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read load response: %w", err)
	}

	responseText := string(body)

	// Parse JSON lines to find loaded images
	lines := strings.Split(responseText, "\n")
	loadedImages := []string{}
	allMessages := []string{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse each JSON line
		var jsonResponse struct {
			Stream string `json:"stream"`
			Status string `json:"status"`
		}

		if err := json.Unmarshal([]byte(line), &jsonResponse); err != nil {
			// Skip non-JSON lines but log them
			allMessages = append(allMessages, line)
			continue
		}

		// Collect all messages for debugging
		if jsonResponse.Stream != "" {
			allMessages = append(allMessages, jsonResponse.Stream)
		}
		if jsonResponse.Status != "" {
			allMessages = append(allMessages, jsonResponse.Status)
		}

		// Look for "Loaded image:" in the stream field
		if jsonResponse.Stream != "" && strings.HasPrefix(jsonResponse.Stream, "Loaded image:") {
			loadedImage := strings.TrimSpace(strings.TrimPrefix(jsonResponse.Stream, "Loaded image:"))
			loadedImages = append(loadedImages, loadedImage)
		}
	}

	if len(loadedImages) == 0 {
		return fmt.Errorf("no images were loaded from tar file. All messages: %v", allMessages)
	}

	return nil
}

func PushImage(ctx context.Context, cli *client.Client, imageRef string, imageConfig *config.Image) error {
	if imageConfig.RegistryAuth == nil {
		return fmt.Errorf("no registry authentication configured for image %s", imageRef)
	}

	authStr, err := getRegistryAuthString(imageConfig)
	if err != nil {
		return fmt.Errorf("failed to get registry auth for %s: %w", imageRef, err)
	}

	pushResponse, err := cli.ImagePush(ctx, imageRef, image.PushOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		return fmt.Errorf("failed to push image %s: %w", imageRef, err)
	}
	defer pushResponse.Close()

	// Optional: Parse and display push progress
	if _, err := io.Copy(io.Discard, pushResponse); err != nil {
		return fmt.Errorf("error reading push response: %w", err)
	}

	return nil
}
