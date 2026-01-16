package haloy

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/cmdexec"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"golang.org/x/sync/errgroup"
)

func ResolveImageBuilds(targets map[string]config.TargetConfig) (
	builds map[string]*config.Image,
	pushes map[string][]*config.Image,
	uploads map[string][]*config.TargetConfig,
	localBuilds map[string][]*config.TargetConfig,
) {
	builds = make(map[string]*config.Image) // imageRef is key
	uploads = make(map[string][]*config.TargetConfig)
	pushes = make(map[string][]*config.Image)
	localBuilds = make(map[string][]*config.TargetConfig)

	for _, target := range targets {
		image := target.Image
		if image == nil || !image.ShouldBuild() {
			continue
		}

		imageRef := image.ImageRef()

		if _, exists := builds[imageRef]; !exists {
			builds[imageRef] = image
		}

		pushStrategy := image.GetEffectivePushStrategy()
		if pushStrategy == config.BuildPushOptionServer {
			if helpers.IsLocalhost(target.Server) {
				// Localhost: image already in shared Docker daemon after build, skip upload
				localBuilds[imageRef] = append(localBuilds[imageRef], &target)
			} else {
				uploads[imageRef] = append(uploads[imageRef], &target)
			}
		} else if pushStrategy == config.BuildPushOptionRegistry && image.RegistryAuth != nil {
			pushes[imageRef] = append(pushes[imageRef], target.Image)
		}
	}

	return builds, pushes, uploads, localBuilds
}

// BuildImage builds a Docker image using the provided image configuration
func BuildImage(ctx context.Context, imageRef string, image *config.Image, configPath string) error {
	ui.Info("Building image %s", imageRef)

	buildConfig := image.BuildConfig
	if buildConfig == nil {
		buildConfig = &config.BuildConfig{}
	}

	// Work directory is the config file's directory.
	// All paths (context, dockerfile) are relative to this directory.
	workDir := getBuilderWorkDir(configPath)

	// Context defaults to "." (config directory) if not specified
	buildContext := "."
	if buildConfig.Context != "" {
		buildContext = buildConfig.Context
	}

	args := []string{"build"}

	if buildConfig.Dockerfile != "" {
		args = append(args, "-f", buildConfig.Dockerfile)
	}

	if buildConfig.Platform == "" {
		buildConfig.Platform = "linux/amd64" // most widely used platform and a common pitfall
	}
	args = append(args, "--platform", buildConfig.Platform)

	for _, buildArg := range buildConfig.Args {
		if buildArg.Value != "" {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%q", buildArg.Name, buildArg.Value))
		} else {
			// If no value specified, pass the build arg name only (Docker will use env var)
			args = append(args, "--build-arg", buildArg.Name)
		}
	}

	// Add image tag
	args = append(args, "-t", imageRef)

	// Add build context as the last argument
	args = append(args, buildContext)

	cmd := fmt.Sprintf("docker %s", strings.Join(args, " "))
	if err := cmdexec.RunCommand(ctx, cmd, workDir); err != nil {
		return fmt.Errorf("failed to build image %s: %w", imageRef, err)
	}

	ui.Success("Successfully built image %s", imageRef)
	return nil
}

// getBuilderWorkDir returns the directory containing the config file.
// All build paths (context, dockerfile) are relative to this directory.
func getBuilderWorkDir(configPath string) string {
	if configPath == "" || configPath == "." {
		return "."
	}
	if stat, err := os.Stat(configPath); err == nil && stat.IsDir() {
		return configPath
	}
	return filepath.Dir(configPath)
}

// UploadImage uploads a Docker image to the specified server
// It tries layer-based upload first (efficient), falls back to full tar upload
func UploadImage(ctx context.Context, imageRef string, resolvedTargetConfigs []*config.TargetConfig) error {
	tempFile, err := os.CreateTemp("", fmt.Sprintf("haloy-upload-%s-*.tar", strings.ReplaceAll(imageRef, ":", "-")))
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	saveCmd := fmt.Sprintf("docker save -o %s %s", tempFile.Name(), imageRef)
	if err := cmdexec.RunCommand(ctx, saveCmd, "."); err != nil {
		return fmt.Errorf("failed to save image to tar: %w", err)
	}

	for _, resolvedDeployConfig := range resolvedTargetConfigs {
		token, err := getToken(resolvedDeployConfig, resolvedDeployConfig.Server)
		if err != nil {
			return fmt.Errorf("failed to get authentication token: %w", err)
		}

		api, err := apiclient.NewWithTimeout(resolvedDeployConfig.Server, token, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		// Check if server supports layer-based upload
		if supportsLayerUpload(ctx, api) {
			ui.Info("Pushing image %s to %s (layered)", imageRef, resolvedDeployConfig.Server)
			if err := uploadImageLayered(ctx, api, imageRef, tempFile.Name()); err != nil {
				// Log the error but fall back to full upload
				ui.Warn("Layer-based push failed, falling back to full push: %v", err)
				ui.Info("Pushing image %s to %s (full)", imageRef, resolvedDeployConfig.Server)
				if err := api.PostFile(ctx, "images/upload", "image", tempFile.Name()); err != nil {
					return fmt.Errorf("failed to upload image: %w", err)
				}
			}
		} else {
			ui.Info("Pushing image %s to %s (full)", imageRef, resolvedDeployConfig.Server)
			if err := api.PostFile(ctx, "images/upload", "image", tempFile.Name()); err != nil {
				return fmt.Errorf("failed to upload image: %w", err)
			}
		}
	}

	return nil
}

// supportsLayerUpload checks if the server supports layer-based upload
func supportsLayerUpload(ctx context.Context, api *apiclient.APIClient) bool {
	var version apitypes.VersionResponse
	if err := api.Get(ctx, "version", &version); err != nil {
		return false
	}
	return slices.Contains(version.Capabilities, "layer-upload")
}

// uploadImageLayered uploads an image using layer-based transfer
func uploadImageLayered(ctx context.Context, api *apiclient.APIClient, imageRef, tarPath string) error {
	// Parse the image tar to extract manifest, config, and layers
	manifest, configData, layers, err := parseImageTar(tarPath)
	if err != nil {
		return fmt.Errorf("failed to parse image tar: %w", err)
	}

	// Extract layer digests for checking
	digests := make([]string, 0, len(layers))
	for digest := range layers {
		digests = append(digests, digest)
	}

	// Check which layers the server already has
	checkReq := apitypes.LayerCheckRequest{Digests: digests}
	var checkResp apitypes.LayerCheckResponse
	if err := api.Post(ctx, "images/layers/check", checkReq, &checkResp); err != nil {
		return fmt.Errorf("failed to check layers: %w", err)
	}

	// Report cache status
	cachedCount := len(checkResp.Exists)
	totalCount := len(digests)
	missingCount := len(checkResp.Missing)

	if missingCount == 0 {
		ui.Info("Server has %d/%d layers cached", cachedCount, totalCount)
	} else {
		if cachedCount > 0 {
			ui.Info("Server has %d/%d layers cached, uploading %d", cachedCount, totalCount, missingCount)
		}

		// Calculate total bytes to upload
		var totalBytes int64
		for _, digest := range checkResp.Missing {
			if info, ok := layers[digest]; ok {
				totalBytes += info.size
			}
		}

		// Create progress bar
		progress := ui.NewProgressBar(ui.ProgressBarConfig{
			Description: "Uploading layers",
			TotalBytes:  totalBytes,
			TotalItems:  missingCount,
			ShowBytes:   true,
		})

		// Upload missing layers in parallel
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(4) // Max 4 concurrent uploads

		for _, digest := range checkResp.Missing {
			layerInfo, ok := layers[digest]
			if !ok {
				progress.Finish()
				return fmt.Errorf("layer %s not found in tar", digest)
			}

			g.Go(func() error {
				// Open the tar and seek to the layer
				layerReader, err := openLayerFromTar(tarPath, layerInfo.tarPath)
				if err != nil {
					return fmt.Errorf("failed to open layer %s: %w", digest, err)
				}

				// Wrap reader to track progress
				trackedReader := &progressReader{
					reader:   layerReader,
					progress: progress,
				}

				// Create and customize the request
				req, err := api.NewRequest(gctx, "POST", "images/layers", trackedReader)
				if err != nil {
					layerReader.Close()
					return fmt.Errorf("failed to create request for layer %s: %w", digest, err)
				}
				req.Header.Set("Content-Type", "application/octet-stream")
				req.Header.Set("X-Layer-Digest", digest)

				resp, err := api.Do(req)
				layerReader.Close()
				if err != nil {
					return fmt.Errorf("failed to upload layer %s: %w", digest, err)
				}

				if resp.StatusCode >= 400 {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					return fmt.Errorf("failed to upload layer %s: server returned %d: %s", digest, resp.StatusCode, string(body))
				}
				resp.Body.Close()

				progress.CompleteItem()
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			progress.Finish()
			return err
		}

		progress.Finish()
		ui.Success("Uploaded %d layers", missingCount)
	}

	// Assemble the image on the server
	assembleReq := apitypes.ImageAssembleRequest{
		ImageRef: imageRef,
		Config:   configData,
		Manifest: manifest,
	}

	var assembleResp apitypes.ImageAssembleResponse
	if err := api.Post(ctx, "images/layers/assemble", assembleReq, &assembleResp); err != nil {
		return fmt.Errorf("failed to assemble image: %w", err)
	}

	return nil
}

// layerInfo holds information about a layer in the tar
type layerInfo struct {
	digest  string
	tarPath string // path within the tar file
	size    int64
}

// parseImageTar extracts manifest, config, and layer info from a docker save tar
func parseImageTar(tarPath string) (apitypes.ImageManifestEntry, []byte, map[string]layerInfo, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return apitypes.ImageManifestEntry{}, nil, nil, err
	}
	defer file.Close()

	tr := tar.NewReader(file)

	var manifestData []byte
	var configData []byte
	var manifest apitypes.ImageManifestEntry
	configName := ""
	layers := make(map[string]layerInfo)

	// First pass: read manifest to know what to look for
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return apitypes.ImageManifestEntry{}, nil, nil, err
		}

		if header.Name == "manifest.json" {
			manifestData, err = io.ReadAll(tr)
			if err != nil {
				return apitypes.ImageManifestEntry{}, nil, nil, err
			}

			var manifests []apitypes.ImageManifestEntry
			if err := json.Unmarshal(manifestData, &manifests); err != nil {
				return apitypes.ImageManifestEntry{}, nil, nil, err
			}
			if len(manifests) == 0 {
				return apitypes.ImageManifestEntry{}, nil, nil, fmt.Errorf("empty manifest")
			}
			manifest = manifests[0]
			configName = manifest.Config

			// Map layer paths to their digests
			for _, layerPath := range manifest.Layers {
				digest := extractDigestFromPath(layerPath)
				layers[digest] = layerInfo{
					digest:  digest,
					tarPath: layerPath,
				}
			}
			break
		}
	}

	if manifestData == nil {
		return apitypes.ImageManifestEntry{}, nil, nil, fmt.Errorf("manifest.json not found in tar")
	}

	// Second pass: read config and get layer sizes
	file.Seek(0, 0)
	tr = tar.NewReader(file)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return apitypes.ImageManifestEntry{}, nil, nil, err
		}

		// Read config
		if header.Name == configName {
			configData, err = io.ReadAll(tr)
			if err != nil {
				return apitypes.ImageManifestEntry{}, nil, nil, err
			}
		}

		// Update layer sizes
		for digest, info := range layers {
			if header.Name == info.tarPath {
				info.size = header.Size
				layers[digest] = info
			}
		}
	}

	if configData == nil {
		return apitypes.ImageManifestEntry{}, nil, nil, fmt.Errorf("config %s not found in tar", configName)
	}

	return manifest, configData, layers, nil
}

// extractDigestFromPath extracts the sha256 digest from a layer path
func extractDigestFromPath(layerPath string) string {
	dir := filepath.Dir(layerPath)

	// Handle modern Docker buildkit OCI format: blobs/sha256/<hash>
	// where the file itself is named with the hash (no layer.tar subdirectory)
	if dir == "blobs/sha256" {
		hash := filepath.Base(layerPath)
		return "sha256:" + hash
	}

	// Handle older buildkit format: blobs/sha256/<hash>/layer.tar
	if strings.HasPrefix(dir, "blobs/sha256/") {
		hash := strings.TrimPrefix(dir, "blobs/sha256/")
		return "sha256:" + hash
	}

	// Handle legacy format: sha256:<hash>/layer.tar
	if strings.HasPrefix(dir, "sha256:") {
		return dir
	}

	// Handle simple format: <hash>/layer.tar
	return "sha256:" + dir
}

// openLayerFromTar opens a specific layer from the tar file for streaming
func openLayerFromTar(tarPath, layerPath string) (io.ReadCloser, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(file)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			file.Close()
			return nil, fmt.Errorf("layer %s not found in tar", layerPath)
		}
		if err != nil {
			file.Close()
			return nil, err
		}

		if header.Name == layerPath {
			// Return a reader that closes the underlying file when done
			return &layerReader{Reader: tr, closer: file}, nil
		}
	}
}

// layerReader wraps a tar reader and closes the underlying file when closed
type layerReader struct {
	io.Reader
	closer io.Closer
}

func (r *layerReader) Close() error {
	return r.closer.Close()
}

// progressReader wraps a reader and reports bytes read to a progress bar
type progressReader struct {
	reader   io.Reader
	progress *ui.ProgressBar
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.progress.Add(int64(n))
	}
	return n, err
}
