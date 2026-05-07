package docker

import (
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/haloydev/haloy/internal/config"
)

func TestIsDockerHubPullRateLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "current docker hub unauthenticated message",
			err:  errors.New("Error response from daemon: error from registry: You have reached your unauthenticated pull rate limit. https://www.docker.com/increase-rate-limit"),
			want: true,
		},
		{
			name: "docker docs pull limit message",
			err:  errors.New("You have reached your pull rate limit. You may increase the limit by authenticating and upgrading: https://www.docker.com/increase-rate-limits"),
			want: true,
		},
		{
			name: "ordinary pull error",
			err:  errors.New("pull access denied, repository does not exist or may require authorization"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDockerHubPullRateLimitError(tt.err)
			if got != tt.want {
				t.Fatalf("isDockerHubPullRateLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldWarnUnauthenticatedDockerHubPull(t *testing.T) {
	tests := []struct {
		name         string
		image        config.Image
		registryAuth string
		want         bool
	}{
		{
			name:  "official docker hub image without auth",
			image: config.Image{Repository: "postgres", Tag: "18"},
			want:  true,
		},
		{
			name:         "official docker hub image with auth",
			image:        config.Image{Repository: "postgres", Tag: "18"},
			registryAuth: "auth",
			want:         false,
		},
		{
			name:  "explicit docker hub image without auth",
			image: config.Image{Repository: "docker.io/library/postgres", Tag: "18"},
			want:  true,
		},
		{
			name:  "non docker hub image without auth",
			image: config.Image{Repository: "ghcr.io/example/postgres", Tag: "18"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldWarnUnauthenticatedDockerHubPull(tt.image, tt.registryAuth)
			if got != tt.want {
				t.Fatalf("shouldWarnUnauthenticatedDockerHubPull() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizedPullRef(t *testing.T) {
	tests := []struct {
		name  string
		image config.Image
		want  string
	}{
		{
			name:  "docker hub official image",
			image: config.Image{Repository: "postgres", Tag: "18"},
			want:  "docker.io/library/postgres:18",
		},
		{
			name:  "docker hub namespaced image",
			image: config.Image{Repository: "library/postgres", Tag: "18"},
			want:  "docker.io/library/postgres:18",
		},
		{
			name:  "explicit docker hub image",
			image: config.Image{Repository: "docker.io/library/postgres", Tag: "18"},
			want:  "docker.io/library/postgres:18",
		},
		{
			name:  "non docker hub image",
			image: config.Image{Repository: "ghcr.io/example/postgres", Tag: "18"},
			want:  "ghcr.io/example/postgres:18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizedPullRef(tt.image); got != tt.want {
				t.Fatalf("normalizedPullRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatImagePullError(t *testing.T) {
	rateLimitErr := errors.New("Error response from daemon: error from registry: You have reached your unauthenticated pull rate limit. https://www.docker.com/increase-rate-limit")

	tests := []struct {
		name            string
		imageRef        string
		image           config.Image
		err             error
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:     "unauthenticated docker hub rate limit",
			imageRef: "postgres:18",
			image:    config.Image{Repository: "postgres", Tag: "18"},
			err:      rateLimitErr,
			wantContains: []string{
				"Docker Hub rate limit reached",
				"without registry credentials",
				"haloy server registry login",
				"image.registry",
				"local docker login is not sent",
			},
			wantNotContains: []string{
				"if you intended to build this image locally",
			},
		},
		{
			name:     "authenticated docker hub rate limit",
			imageRef: "postgres:18",
			image: config.Image{
				Repository: "postgres",
				Tag:        "18",
				RegistryAuth: &config.RegistryAuth{
					Username: config.ValueSource{Value: "user"},
					Password: config.ValueSource{Value: "token"},
				},
			},
			err: rateLimitErr,
			wantContains: []string{
				"Docker Hub rate limit reached",
				"using configured registry credentials",
			},
			wantNotContains: []string{
				"without registry credentials",
				"if you intended to build this image locally",
			},
		},
		{
			name:     "non docker hub rate limit",
			imageRef: "ghcr.io/example/postgres:18",
			image:    config.Image{Repository: "ghcr.io/example/postgres", Tag: "18"},
			err:      rateLimitErr,
			wantContains: []string{
				"failed to pull ghcr.io/example/postgres:18",
			},
			wantNotContains: []string{
				"Docker Hub rate limit reached",
				"if you intended to build this image locally",
			},
		},
		{
			name:     "shorthand ordinary pull error keeps build hint",
			imageRef: "myapp:latest",
			image:    config.Image{Repository: "myapp"},
			err:      errors.New("pull access denied, repository does not exist or may require authorization"),
			wantContains: []string{
				"failed to pull myapp:latest",
				"if you intended to build this image locally",
			},
			wantNotContains: []string{
				"Docker Hub rate limit reached",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatImagePullError(tt.imageRef, tt.image, tt.err).Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Fatalf("formatImagePullError() = %q, want to contain %q", got, want)
				}
			}
			for _, unwanted := range tt.wantNotContains {
				if strings.Contains(got, unwanted) {
					t.Fatalf("formatImagePullError() = %q, want not to contain %q", got, unwanted)
				}
			}
		})
	}
}

func TestSelectImageTagsToRemove_IgnoreDeploymentCountsTowardKeepLimit(t *testing.T) {
	candidates := []removableImageTag{
		{Tag: "app:20260222010101", DeploymentID: "20260222010101", ImageID: "img-1"},
		{Tag: "app:20260222010102", DeploymentID: "20260222010102", ImageID: "img-2"},
		{Tag: "app:20260222010103", DeploymentID: "20260222010103", ImageID: "img-3"},
	}

	removals := selectImageTagsToRemove(candidates, map[string]struct{}{}, 2, "20260222010104")

	if len(removals) != 2 {
		t.Fatalf("len(removals) = %d, want 2", len(removals))
	}
	if removals[0].Tag != "app:20260222010102" {
		t.Fatalf("removals[0].Tag = %q, want %q", removals[0].Tag, "app:20260222010102")
	}
	if removals[1].Tag != "app:20260222010101" {
		t.Fatalf("removals[1].Tag = %q, want %q", removals[1].Tag, "app:20260222010101")
	}
}

func TestSelectImageTagsToRemove_KeepCurrentOnlyRemovesOlderTags(t *testing.T) {
	candidates := []removableImageTag{
		{Tag: "app:20260222010101", DeploymentID: "20260222010101", ImageID: "img-1"},
		{Tag: "app:20260222010102", DeploymentID: "20260222010102", ImageID: "img-2"},
	}

	removals := selectImageTagsToRemove(candidates, map[string]struct{}{}, 1, "20260222010103")

	if len(removals) != 2 {
		t.Fatalf("len(removals) = %d, want 2", len(removals))
	}
	if removals[0].Tag != "app:20260222010102" {
		t.Fatalf("removals[0].Tag = %q, want %q", removals[0].Tag, "app:20260222010102")
	}
	if removals[1].Tag != "app:20260222010101" {
		t.Fatalf("removals[1].Tag = %q, want %q", removals[1].Tag, "app:20260222010101")
	}
}

func TestSelectImageTagsToRemove_PreservesInUseImageWithoutKeptTag(t *testing.T) {
	candidates := []removableImageTag{
		{Tag: "app:20260222010101", DeploymentID: "20260222010101", ImageID: "img-1"},
	}

	removals := selectImageTagsToRemove(candidates, map[string]struct{}{"img-1": {}}, 0, "20260222010102")

	if len(removals) != 0 {
		t.Fatalf("len(removals) = %d, want 0", len(removals))
	}
}

func TestPlanImagePrune_ReturnsRunningDeploymentsAndRemovableTags(t *testing.T) {
	candidates := []removableImageTag{
		{Tag: "app:20260222010101", DeploymentID: "20260222010101", ImageID: "img-1"},
		{Tag: "app:20260222010102", DeploymentID: "20260222010102", ImageID: "img-2"},
		{Tag: "app:20260222010103", DeploymentID: "20260222010103", ImageID: "img-3"},
	}

	plan := planImagePrune(
		candidates,
		map[string]struct{}{"img-3": {}},
		"app",
		"",
		1,
		[]string{"20260222010103"},
	)

	if plan.AppName != "app" {
		t.Fatalf("plan.AppName = %q, want %q", plan.AppName, "app")
	}
	if plan.Keep != 1 {
		t.Fatalf("plan.Keep = %d, want %d", plan.Keep, 1)
	}
	if len(plan.RunningDeploymentIDs) != 1 || plan.RunningDeploymentIDs[0] != "20260222010103" {
		t.Fatalf("plan.RunningDeploymentIDs = %v, want [20260222010103]", plan.RunningDeploymentIDs)
	}
	if len(plan.Tags) != 2 {
		t.Fatalf("len(plan.Tags) = %d, want 2", len(plan.Tags))
	}
	if plan.Tags[0].Tag != "app:20260222010102" {
		t.Fatalf("plan.Tags[0].Tag = %q, want %q", plan.Tags[0].Tag, "app:20260222010102")
	}
	if plan.Tags[1].Tag != "app:20260222010101" {
		t.Fatalf("plan.Tags[1].Tag = %q, want %q", plan.Tags[1].Tag, "app:20260222010101")
	}
}

func TestRunningDeploymentIDs_DeduplicatesAndSortsDescending(t *testing.T) {
	containers := []container.Summary{
		{Labels: map[string]string{config.LabelDeploymentID: "20260222010102"}},
		{Labels: map[string]string{config.LabelDeploymentID: "20260222010101"}},
		{Labels: map[string]string{config.LabelDeploymentID: "20260222010102"}},
	}

	ids := runningDeploymentIDs(containers)

	if len(ids) != 2 {
		t.Fatalf("len(ids) = %d, want 2", len(ids))
	}
	if ids[0] != "20260222010102" || ids[1] != "20260222010101" {
		t.Fatalf("ids = %v, want [20260222010102 20260222010101]", ids)
	}
}
