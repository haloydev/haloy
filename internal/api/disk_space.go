package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/system"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/layerstore"
	"github.com/haloydev/haloy/internal/storage"
	"golang.org/x/sys/unix"
)

const assembledLayerMetadataOverheadBytes uint64 = 4096

type diskSpaceProbe interface {
	FilesystemInfo(path string) (filesystemInfo, error)
}

type dockerInfoProvider interface {
	Info(ctx context.Context) (system.Info, error)
	Close() error
}

type layerPathResolver interface {
	GetLayerPath(digest string) (string, error)
}

type filesystemInfo struct {
	Path           string
	AvailableBytes uint64
	DeviceID       uint64
}

type diskSpaceRequirement struct {
	Path           string
	AvailableBytes uint64
	RequiredBytes  uint64
}

type diskSpaceCheckResult struct {
	Path           string
	AvailableBytes uint64
	RequiredBytes  uint64
	OK             bool
}

type diskSpaceContribution struct {
	Path  string
	Bytes uint64
}

type insufficientDiskSpaceError struct {
	Path           string
	AvailableBytes uint64
	RequiredBytes  uint64
}

func (e *insufficientDiskSpaceError) Error() string {
	return fmt.Sprintf(
		"insufficient disk space on %s: need %s free, have %s free",
		e.Path,
		helpers.FormatBinaryBytes(e.RequiredBytes),
		helpers.FormatBinaryBytes(e.AvailableBytes),
	)
}

type osDiskSpaceProbe struct{}

func (osDiskSpaceProbe) FilesystemInfo(path string) (filesystemInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filesystemInfo{}, fmt.Errorf("resolve path %s: %w", path, err)
	}

	statPath, err := nearestExistingPath(absPath)
	if err != nil {
		return filesystemInfo{}, err
	}

	var stat unix.Stat_t
	if err := unix.Stat(statPath, &stat); err != nil {
		return filesystemInfo{}, fmt.Errorf("stat %s: %w", statPath, err)
	}

	var statfs unix.Statfs_t
	if err := unix.Statfs(statPath, &statfs); err != nil {
		return filesystemInfo{}, fmt.Errorf("statfs %s: %w", statPath, err)
	}

	return filesystemInfo{
		Path:           statPath,
		AvailableBytes: statfs.Bavail * uint64(statfs.Bsize),
		DeviceID:       uint64(stat.Dev),
	}, nil
}

func nearestExistingPath(path string) (string, error) {
	current := path
	for {
		if _, err := os.Stat(current); err == nil {
			return current, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing path found for %s", path)
		}
		current = parent
	}
}

func (s *APIServer) ensureUploadDiskSpace(ctx context.Context, uploadSize int64) error {
	if s.uploadDiskSpaceCheck != nil {
		return s.uploadDiskSpaceCheck(ctx, uploadSize)
	}

	return ensureDiskSpaceForUpload(ctx, osDiskSpaceProbe{}, uploadSize)
}

func (s *APIServer) ensureAssembleDiskSpace(ctx context.Context, req apitypes.ImageAssembleRequest) error {
	if s.assembleDiskSpaceCheck != nil {
		return s.assembleDiskSpaceCheck(ctx, req)
	}

	db, err := storage.New()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	store, err := layerstore.New(db)
	if err != nil {
		return fmt.Errorf("initialize layer store: %w", err)
	}

	return ensureDiskSpaceForAssemble(ctx, osDiskSpaceProbe{}, store, req)
}

func ensureDiskSpaceForUpload(ctx context.Context, probe diskSpaceProbe, uploadSize int64) error {
	if uploadSize <= 0 {
		return nil
	}

	cli, err := docker.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}
	defer cli.Close()

	return ensureDiskSpaceForUploadWithDocker(ctx, cli, probe, uint64(uploadSize))
}

func ensureDiskSpaceForUploadWithDocker(ctx context.Context, cli dockerInfoProvider, probe diskSpaceProbe, uploadSize uint64) error {
	result, err := checkDiskSpaceForUploadWithDocker(ctx, cli, probe, uploadSize)
	if err != nil {
		return err
	}

	return result.Err()
}

func ensureDiskSpaceForAssemble(ctx context.Context, probe diskSpaceProbe, store layerPathResolver, req apitypes.ImageAssembleRequest) error {
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}
	defer cli.Close()

	return ensureDiskSpaceForAssembleWithDocker(ctx, cli, probe, store, req)
}

func ensureDiskSpaceForAssembleWithDocker(ctx context.Context, cli dockerInfoProvider, probe diskSpaceProbe, store layerPathResolver, req apitypes.ImageAssembleRequest) error {
	estimatedTarBytes, err := estimateAssembledImageTarSize(store, req)
	if err != nil {
		return err
	}

	result, err := checkDiskSpaceForLayeredUploadWithDocker(ctx, cli, probe, 0, estimatedTarBytes)
	if err != nil {
		return err
	}

	return result.Err()
}

func checkDiskSpaceForUpload(ctx context.Context, probe diskSpaceProbe, uploadSize uint64) (diskSpaceCheckResult, error) {
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return diskSpaceCheckResult{}, fmt.Errorf("create Docker client: %w", err)
	}
	defer cli.Close()

	return checkDiskSpaceForUploadWithDocker(ctx, cli, probe, uploadSize)
}

func checkDiskSpaceForUploadWithDocker(ctx context.Context, cli dockerInfoProvider, probe diskSpaceProbe, uploadSize uint64) (diskSpaceCheckResult, error) {
	requirements, err := uploadDiskSpaceRequirements(ctx, cli, probe, uploadSize)
	if err != nil {
		return diskSpaceCheckResult{}, err
	}

	return summarizeDiskSpaceRequirements(requirements), nil
}

func checkDiskSpaceForLayeredUpload(ctx context.Context, probe diskSpaceProbe, layerUploadBytes, assembledImageSizeBytes uint64) (diskSpaceCheckResult, error) {
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return diskSpaceCheckResult{}, fmt.Errorf("create Docker client: %w", err)
	}
	defer cli.Close()

	return checkDiskSpaceForLayeredUploadWithDocker(ctx, cli, probe, layerUploadBytes, assembledImageSizeBytes)
}

func checkDiskSpaceForLayeredUploadWithDocker(ctx context.Context, cli dockerInfoProvider, probe diskSpaceProbe, layerUploadBytes, assembledImageSizeBytes uint64) (diskSpaceCheckResult, error) {
	requirements, err := layeredUploadDiskSpaceRequirements(ctx, cli, probe, layerUploadBytes, assembledImageSizeBytes)
	if err != nil {
		return diskSpaceCheckResult{}, err
	}

	return summarizeDiskSpaceRequirements(requirements), nil
}

func uploadDiskSpaceRequirements(ctx context.Context, cli dockerInfoProvider, probe diskSpaceProbe, uploadSize uint64) ([]diskSpaceRequirement, error) {
	if uploadSize == 0 {
		return nil, nil
	}

	tempDir, err := config.ImageTempDirPath()
	if err != nil {
		return nil, err
	}
	dockerRootDir, err := dockerRootDir(ctx, cli)
	if err != nil {
		return nil, err
	}

	return buildDiskSpaceRequirements(probe, constants.DefaultImageDiskReserve,
		diskSpaceContribution{Path: tempDir, Bytes: uploadSize},
		diskSpaceContribution{Path: dockerRootDir, Bytes: uploadSize},
	)
}

func layeredUploadDiskSpaceRequirements(ctx context.Context, cli dockerInfoProvider, probe diskSpaceProbe, layerUploadBytes, assembledImageSizeBytes uint64) ([]diskSpaceRequirement, error) {
	if assembledImageSizeBytes == 0 && layerUploadBytes == 0 {
		return nil, nil
	}

	tempDir, err := config.ImageTempDirPath()
	if err != nil {
		return nil, err
	}
	dockerRootDir, err := dockerRootDir(ctx, cli)
	if err != nil {
		return nil, err
	}

	layerStorageDir, err := layerStoragePath()
	if err != nil {
		return nil, err
	}

	return buildDiskSpaceRequirements(probe, constants.DefaultImageDiskReserve,
		diskSpaceContribution{Path: layerStorageDir, Bytes: layerUploadBytes},
		diskSpaceContribution{Path: tempDir, Bytes: assembledImageSizeBytes},
		diskSpaceContribution{Path: dockerRootDir, Bytes: assembledImageSizeBytes},
	)
}

func dockerRootDir(ctx context.Context, cli dockerInfoProvider) (string, error) {
	dockerInfo, err := cli.Info(ctx)
	if err != nil {
		return "", fmt.Errorf("read Docker info: %w", err)
	}
	if dockerInfo.DockerRootDir == "" {
		return "", fmt.Errorf("docker root directory is empty")
	}
	return dockerInfo.DockerRootDir, nil
}

func layerStoragePath() (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	return filepath.Join(dataDir, constants.LayersDir), nil
}

func buildDiskSpaceRequirements(probe diskSpaceProbe, reserveBytes uint64, contributions ...diskSpaceContribution) ([]diskSpaceRequirement, error) {
	byDevice := make(map[uint64]diskSpaceRequirement)

	for _, contribution := range contributions {
		if contribution.Bytes == 0 {
			continue
		}

		fs, err := probe.FilesystemInfo(contribution.Path)
		if err != nil {
			return nil, fmt.Errorf("inspect filesystem %s: %w", contribution.Path, err)
		}

		requirement, exists := byDevice[fs.DeviceID]
		if !exists {
			requirement = diskSpaceRequirement{
				Path:           fs.Path,
				AvailableBytes: fs.AvailableBytes,
				RequiredBytes:  reserveBytes,
			}
		}

		requirement.RequiredBytes += contribution.Bytes
		if fs.AvailableBytes < requirement.AvailableBytes {
			requirement.AvailableBytes = fs.AvailableBytes
			requirement.Path = fs.Path
		}

		byDevice[fs.DeviceID] = requirement
	}

	requirements := make([]diskSpaceRequirement, 0, len(byDevice))
	for _, requirement := range byDevice {
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

func summarizeDiskSpaceRequirements(requirements []diskSpaceRequirement) diskSpaceCheckResult {
	if len(requirements) == 0 {
		return diskSpaceCheckResult{OK: true}
	}

	limiting := requirements[0]
	for _, req := range requirements[1:] {
		if requirementScore(req) < requirementScore(limiting) {
			limiting = req
		}
	}

	return diskSpaceCheckResult{
		Path:           limiting.Path,
		AvailableBytes: limiting.AvailableBytes,
		RequiredBytes:  limiting.RequiredBytes,
		OK:             limiting.AvailableBytes >= limiting.RequiredBytes,
	}
}

func requirementScore(req diskSpaceRequirement) int64 {
	if req.AvailableBytes >= req.RequiredBytes {
		return int64(req.AvailableBytes - req.RequiredBytes)
	}
	return -int64(req.RequiredBytes - req.AvailableBytes)
}

func (r diskSpaceCheckResult) Err() error {
	if r.OK {
		return nil
	}
	return &insufficientDiskSpaceError{
		Path:           r.Path,
		AvailableBytes: r.AvailableBytes,
		RequiredBytes:  r.RequiredBytes,
	}
}

func estimateAssembledImageTarSize(store layerPathResolver, req apitypes.ImageAssembleRequest) (uint64, error) {
	manifestJSON, err := json.Marshal([]apitypes.ImageManifestEntry{req.Manifest})
	if err != nil {
		return 0, fmt.Errorf("marshal manifest: %w", err)
	}

	total := uint64(len(req.Config)) + uint64(len(manifestJSON))

	for _, layerPath := range req.Manifest.Layers {
		digest, err := digestFromLayerPath(layerPath)
		if err != nil {
			return 0, err
		}

		storedLayerPath, err := store.GetLayerPath(digest)
		if err != nil {
			return 0, err
		}

		info, err := os.Stat(storedLayerPath)
		if err != nil {
			return 0, fmt.Errorf("stat layer %s: %w", digest, err)
		}

		total += uint64(info.Size())
		total += assembledLayerMetadataOverheadBytes
	}

	return total, nil
}

func digestFromLayerPath(layerPath string) (string, error) {
	dir := filepath.Dir(layerPath)

	switch {
	case dir == "blobs/sha256":
		hash := filepath.Base(layerPath)
		if hash == "." || hash == "/" || hash == "" {
			return "", fmt.Errorf("invalid layer path: %s", layerPath)
		}
		return "sha256:" + hash, nil
	case strings.HasPrefix(dir, "blobs/sha256/"):
		return "sha256:" + strings.TrimPrefix(dir, "blobs/sha256/"), nil
	case strings.HasPrefix(dir, "sha256:"):
		return dir, nil
	case dir == "." || dir == "/" || dir == "":
		return "", fmt.Errorf("invalid layer path: %s", layerPath)
	default:
		return "sha256:" + dir, nil
	}
}

func writeImageHandlerError(w http.ResponseWriter, prefix string, err error) {
	var diskErr *insufficientDiskSpaceError
	if errors.As(err, &diskErr) {
		http.Error(w, diskErr.Error(), http.StatusInsufficientStorage)
		return
	}

	http.Error(w, fmt.Sprintf("%s: %v", prefix, err), http.StatusInternalServerError)
}
