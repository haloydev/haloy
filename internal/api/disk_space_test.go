package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/system"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

type fakeDiskSpaceProbe struct {
	infos map[string]filesystemInfo
}

func (f fakeDiskSpaceProbe) FilesystemInfo(path string) (filesystemInfo, error) {
	info, ok := f.infos[path]
	if !ok {
		return filesystemInfo{}, os.ErrNotExist
	}
	return info, nil
}

type fakeDockerInfoClient struct {
	rootDir string
}

func (f fakeDockerInfoClient) Info(context.Context) (system.Info, error) {
	return system.Info{DockerRootDir: f.rootDir}, nil
}

func (f fakeDockerInfoClient) Close() error {
	return nil
}

type fakeLayerStore struct {
	paths map[string]string
}

func (f fakeLayerStore) GetLayerPath(digest string) (string, error) {
	path, ok := f.paths[digest]
	if !ok {
		return "", os.ErrNotExist
	}
	return path, nil
}

func TestEnsureDiskSpaceForUpload_SameFilesystemCountsTempAndDockerTogether(t *testing.T) {
	tempDir, err := config.ImageTempDirPath()
	if err != nil {
		t.Fatalf("ImageTempDirPath error = %v", err)
	}
	dockerRoot := "/var/lib/docker"
	uploadSize := uint64(1024)
	requiredBytes := uploadSize*2 + constants.DefaultImageDiskReserve

	err = ensureDiskSpaceForUploadWithDocker(
		context.Background(),
		fakeDockerInfoClient{rootDir: dockerRoot},
		fakeDiskSpaceProbe{infos: map[string]filesystemInfo{
			tempDir:    {Path: tempDir, AvailableBytes: requiredBytes - 1, DeviceID: 7},
			dockerRoot: {Path: dockerRoot, AvailableBytes: requiredBytes - 1, DeviceID: 7},
		}},
		uploadSize,
	)
	if err == nil {
		t.Fatal("expected insufficient disk space error")
	}

	var diskErr *insufficientDiskSpaceError
	if !strings.Contains(err.Error(), "insufficient disk space") || !errors.As(err, &diskErr) {
		t.Fatalf("err = %v, want insufficient disk space error", err)
	}
	if diskErr.RequiredBytes != requiredBytes {
		t.Fatalf("required bytes = %d, want %d", diskErr.RequiredBytes, requiredBytes)
	}
}

func TestEnsureDiskSpaceForUpload_DifferentFilesystemsChecksSeparately(t *testing.T) {
	tempDir, err := config.ImageTempDirPath()
	if err != nil {
		t.Fatalf("ImageTempDirPath error = %v", err)
	}
	dockerRoot := "/var/lib/docker"
	uploadSize := uint64(2048)
	requiredBytes := uploadSize + constants.DefaultImageDiskReserve

	err = ensureDiskSpaceForUploadWithDocker(
		context.Background(),
		fakeDockerInfoClient{rootDir: dockerRoot},
		fakeDiskSpaceProbe{infos: map[string]filesystemInfo{
			tempDir:    {Path: tempDir, AvailableBytes: requiredBytes + 1, DeviceID: 1},
			dockerRoot: {Path: dockerRoot, AvailableBytes: requiredBytes - 1, DeviceID: 2},
		}},
		uploadSize,
	)
	if err == nil {
		t.Fatal("expected insufficient disk space error")
	}

	var diskErr *insufficientDiskSpaceError
	if !errors.As(err, &diskErr) {
		t.Fatalf("err = %v, want insufficient disk space error", err)
	}
	if diskErr.Path != dockerRoot {
		t.Fatalf("path = %s, want %s", diskErr.Path, dockerRoot)
	}
}

func TestEstimateAssembledImageTarSize(t *testing.T) {
	tempDir := t.TempDir()
	layerOne := writeSizedFile(t, filepath.Join(tempDir, "layer-one.tar"), 10)
	layerTwo := writeSizedFile(t, filepath.Join(tempDir, "layer-two.tar"), 20)

	req := apitypes.ImageAssembleRequest{
		Config: []byte("config"),
		Manifest: apitypes.ImageManifestEntry{
			Config: "config.json",
			Layers: []string{
				"sha256:layerone/layer.tar",
				"sha256:layertwo/layer.tar",
			},
		},
	}

	size, err := estimateAssembledImageTarSize(fakeLayerStore{paths: map[string]string{
		"sha256:layerone": layerOne,
		"sha256:layertwo": layerTwo,
	}}, req)
	if err != nil {
		t.Fatalf("estimateAssembledImageTarSize error = %v", err)
	}

	manifestJSON, err := json.Marshal([]apitypes.ImageManifestEntry{req.Manifest})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	want := uint64(len(req.Config) + len(manifestJSON) + 10 + 20)
	want += 2 * assembledLayerMetadataOverheadBytes
	if size != want {
		t.Fatalf("size = %d, want %d", size, want)
	}
}

func TestEnsureDiskSpaceForAssemble_UsesEstimatedTarSize(t *testing.T) {
	tempDir := t.TempDir()
	layerOne := writeSizedFile(t, filepath.Join(tempDir, "layer-one.tar"), 15)
	layerTwo := writeSizedFile(t, filepath.Join(tempDir, "layer-two.tar"), 25)

	req := apitypes.ImageAssembleRequest{
		Config: []byte("config"),
		Manifest: apitypes.ImageManifestEntry{
			Config: "config.json",
			Layers: []string{
				"sha256:layerone/layer.tar",
				"sha256:layertwo/layer.tar",
			},
		},
	}

	store := fakeLayerStore{paths: map[string]string{
		"sha256:layerone": layerOne,
		"sha256:layertwo": layerTwo,
	}}

	estimatedBytes, err := estimateAssembledImageTarSize(store, req)
	if err != nil {
		t.Fatalf("estimateAssembledImageTarSize error = %v", err)
	}

	tempDirPath, err := config.ImageTempDirPath()
	if err != nil {
		t.Fatalf("ImageTempDirPath error = %v", err)
	}

	dockerRoot := "/var/lib/docker"
	err = ensureDiskSpaceForAssembleWithDocker(
		context.Background(),
		fakeDockerInfoClient{rootDir: dockerRoot},
		fakeDiskSpaceProbe{infos: map[string]filesystemInfo{
			tempDirPath: {Path: tempDirPath, AvailableBytes: estimatedBytes + constants.DefaultImageDiskReserve + 1, DeviceID: 1},
			dockerRoot:  {Path: dockerRoot, AvailableBytes: estimatedBytes + constants.DefaultImageDiskReserve - 1, DeviceID: 2},
		}},
		store,
		req,
	)
	if err == nil {
		t.Fatal("expected insufficient disk space error")
	}
}

func TestWriteImageHandlerError_Uses507ForDiskSpace(t *testing.T) {
	rr := httptest.NewRecorder()

	writeImageHandlerError(rr, "Failed disk space preflight", &insufficientDiskSpaceError{
		Path:           "/tmp",
		RequiredBytes:  1024,
		AvailableBytes: 512,
	})

	if rr.Code != http.StatusInsufficientStorage {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInsufficientStorage)
	}
	if !strings.Contains(rr.Body.String(), "insufficient disk space") {
		t.Fatalf("body = %q, want disk space error", rr.Body.String())
	}
}

func writeSizedFile(t *testing.T, path string, size int) string {
	t.Helper()

	data := bytes.Repeat([]byte("x"), size)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
	return path
}
