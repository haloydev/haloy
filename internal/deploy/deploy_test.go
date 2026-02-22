package deploy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/storage"
)

type fakeDockerOps struct {
	calls                  []string
	tagResult              string
	ensureUpToDateErr      error
	tagErr                 error
	stopErr                error
	removeContainersErr    error
	ensureVolumesErr       error
	runContainerResult     []docker.ContainerRunResult
	runContainerErr        error
	removeImagesErr        error
	lastRemoveImagesKeep   int
	lastRemoveImagesApp    string
	lastRemoveImagesIgnore string
}

func (f *fakeDockerOps) EnsureImageUpToDate(context.Context, *slog.Logger, config.Image) error {
	f.calls = append(f.calls, "EnsureImageUpToDate")
	return f.ensureUpToDateErr
}

func (f *fakeDockerOps) TagImage(context.Context, string, string, string) (string, error) {
	f.calls = append(f.calls, "TagImage")
	if f.tagErr != nil {
		return "", f.tagErr
	}
	if f.tagResult != "" {
		return f.tagResult, nil
	}
	return "app:dep", nil
}

func (f *fakeDockerOps) StopContainers(context.Context, *slog.Logger, string, string) ([]string, error) {
	f.calls = append(f.calls, "StopContainers")
	if f.stopErr != nil {
		return nil, f.stopErr
	}
	return nil, nil
}

func (f *fakeDockerOps) RemoveContainers(context.Context, *slog.Logger, string, string) ([]string, error) {
	f.calls = append(f.calls, "RemoveContainers")
	if f.removeContainersErr != nil {
		return nil, f.removeContainersErr
	}
	return nil, nil
}

func (f *fakeDockerOps) EnsureVolumes(context.Context, *slog.Logger, string, []string) error {
	f.calls = append(f.calls, "EnsureVolumes")
	return f.ensureVolumesErr
}

func (f *fakeDockerOps) RunContainer(context.Context, string, string, config.TargetConfig) ([]docker.ContainerRunResult, error) {
	f.calls = append(f.calls, "RunContainer")
	if f.runContainerErr != nil {
		return nil, f.runContainerErr
	}
	return f.runContainerResult, nil
}

func (f *fakeDockerOps) RemoveImages(_ context.Context, _ *slog.Logger, appName, ignoreDeploymentID string, deploymentsToKeep int) error {
	f.calls = append(f.calls, "RemoveImages")
	f.lastRemoveImagesKeep = deploymentsToKeep
	f.lastRemoveImagesApp = appName
	f.lastRemoveImagesIgnore = ignoreDeploymentID
	return f.removeImagesErr
}

type fakeStore struct {
	saved      []storage.Deployment
	pruneCalls []int
	appNames   []string
	saveErr    error
	pruneErr   error
	closed     bool
}

func (f *fakeStore) SaveDeployment(d storage.Deployment) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, d)
	return nil
}

func (f *fakeStore) PruneOldDeployments(appName string, deploymentsToKeep int) error {
	if f.pruneErr != nil {
		return f.pruneErr
	}
	f.appNames = append(f.appNames, appName)
	f.pruneCalls = append(f.pruneCalls, deploymentsToKeep)
	return nil
}

func (f *fakeStore) Close() error {
	f.closed = true
	return nil
}

type fakeStoreFactory struct {
	store   *fakeStore
	openErr error
}

func (f *fakeStoreFactory) Open() (deploymentStore, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.store, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func targetConfigWithHistory(strategy config.HistoryStrategy, count *int) config.TargetConfig {
	replicas := 1
	return config.TargetConfig{
		Name: "app",
		Image: &config.Image{
			Repository: "nginx",
			Tag:        "1.25",
			History: &config.ImageHistory{
				Strategy: strategy,
				Count:    count,
			},
		},
		Replicas: replicasPtr(replicas),
	}
}

func rawConfigWithHistory(strategy config.HistoryStrategy, count *int) config.DeployConfig {
	return config.DeployConfig{
		TargetConfig: config.TargetConfig{
			Name: "app",
			Image: &config.Image{
				Repository: "nginx",
				Tag:        "1.25",
				History: &config.ImageHistory{
					Strategy: strategy,
					Count:    count,
				},
			},
		},
	}
}

func replicasPtr(v int) *int { return &v }

func TestDeployAppWithDeps_ReplaceAndLocalHistory(t *testing.T) {
	keep := 3
	dockerFake := &fakeDockerOps{
		runContainerResult: []docker.ContainerRunResult{{ID: "c1"}},
		tagResult:          "app:dep-1",
	}
	store := &fakeStore{}
	factory := &fakeStoreFactory{store: store}

	target := targetConfigWithHistory(config.HistoryStrategyLocal, &keep)
	target.DeploymentStrategy = config.DeploymentStrategyReplace
	raw := rawConfigWithHistory(config.HistoryStrategyLocal, &keep)

	err := deployAppWithDeps(context.Background(), "dep-1", target, raw, testLogger(), dockerFake, factory)
	if err != nil {
		t.Fatalf("deployAppWithDeps() error = %v", err)
	}

	wantCalls := []string{"EnsureImageUpToDate", "TagImage", "StopContainers", "RemoveContainers", "RunContainer", "RemoveImages"}
	if len(dockerFake.calls) != len(wantCalls) {
		t.Fatalf("docker call count = %d, want %d (%v)", len(dockerFake.calls), len(wantCalls), dockerFake.calls)
	}
	for i, call := range wantCalls {
		if dockerFake.calls[i] != call {
			t.Fatalf("docker call[%d] = %s, want %s", i, dockerFake.calls[i], call)
		}
	}

	if dockerFake.lastRemoveImagesKeep != keep {
		t.Fatalf("RemoveImages keep = %d, want %d", dockerFake.lastRemoveImagesKeep, keep)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved deployments = %d, want 1", len(store.saved))
	}
	if len(store.pruneCalls) != 1 || store.pruneCalls[0] != keep {
		t.Fatalf("prune calls = %v, want [%d]", store.pruneCalls, keep)
	}
}

func TestDeployAppWithDeps_HistoryNoneSkipsTagAndStorage(t *testing.T) {
	keep := 2
	dockerFake := &fakeDockerOps{runContainerResult: []docker.ContainerRunResult{{ID: "c1"}}}
	store := &fakeStore{}
	factory := &fakeStoreFactory{store: store}

	target := targetConfigWithHistory(config.HistoryStrategyNone, &keep)
	raw := rawConfigWithHistory(config.HistoryStrategyNone, &keep)

	err := deployAppWithDeps(context.Background(), "dep-1", target, raw, testLogger(), dockerFake, factory)
	if err != nil {
		t.Fatalf("deployAppWithDeps() error = %v", err)
	}

	for _, call := range dockerFake.calls {
		if call == "TagImage" {
			t.Fatal("TagImage should not be called for history strategy none")
		}
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved deployments = %d, want 0", len(store.saved))
	}
	if len(store.pruneCalls) != 0 {
		t.Fatalf("prune calls = %v, want none", store.pruneCalls)
	}
}

func TestDeployAppWithDeps_RunContainerTimeout(t *testing.T) {
	keep := 2
	dockerFake := &fakeDockerOps{runContainerErr: context.DeadlineExceeded}
	store := &fakeStore{}
	factory := &fakeStoreFactory{store: store}

	target := targetConfigWithHistory(config.HistoryStrategyLocal, &keep)
	raw := rawConfigWithHistory(config.HistoryStrategyLocal, &keep)

	err := deployAppWithDeps(context.Background(), "dep-1", target, raw, testLogger(), dockerFake, factory)
	if err == nil {
		t.Fatal("deployAppWithDeps() expected timeout error")
	}
	if !strings.Contains(err.Error(), "container startup timed out") {
		t.Fatalf("error = %v, expected timeout wrapper", err)
	}
}

func TestDeployAppWithDeps_CanceledContext(t *testing.T) {
	keep := 2
	dockerFake := &fakeDockerOps{runContainerErr: context.Canceled}
	store := &fakeStore{}
	factory := &fakeStoreFactory{store: store}

	target := targetConfigWithHistory(config.HistoryStrategyLocal, &keep)
	raw := rawConfigWithHistory(config.HistoryStrategyLocal, &keep)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := deployAppWithDeps(ctx, "dep-1", target, raw, testLogger(), dockerFake, factory)
	if err == nil {
		t.Fatal("deployAppWithDeps() expected canceled error")
	}
	if !strings.Contains(err.Error(), "deployment canceled") {
		t.Fatalf("error = %v, expected deployment canceled wrapper", err)
	}
}

func TestDeployAppWithDeps_NoContainersStarted(t *testing.T) {
	keep := 2
	dockerFake := &fakeDockerOps{runContainerResult: []docker.ContainerRunResult{}}
	store := &fakeStore{}
	factory := &fakeStoreFactory{store: store}

	target := targetConfigWithHistory(config.HistoryStrategyLocal, &keep)
	raw := rawConfigWithHistory(config.HistoryStrategyLocal, &keep)

	err := deployAppWithDeps(context.Background(), "dep-1", target, raw, testLogger(), dockerFake, factory)
	if err == nil {
		t.Fatal("deployAppWithDeps() expected no containers started error")
	}
	if !strings.Contains(err.Error(), "no containers started") {
		t.Fatalf("error = %v, expected no containers started message", err)
	}
}

func TestWriteDeployConfigHistory_StoreOpenError(t *testing.T) {
	keep := 2
	raw := rawConfigWithHistory(config.HistoryStrategyLocal, &keep)
	factory := &fakeStoreFactory{openErr: errors.New("open failed")}

	err := writeDeployConfigHistory(raw, "dep-1", "app:dep-1", factory)
	if err == nil {
		t.Fatal("writeDeployConfigHistory() expected store open error")
	}
	if !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("error = %v, expected open failed", err)
	}
}
