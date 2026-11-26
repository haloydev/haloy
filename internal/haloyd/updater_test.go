package haloyd

import (
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

func healthyContainerFor(appName, deploymentID, containerID string) HealthyContainer {
	return HealthyContainer{
		ContainerID: containerID,
		Labels: &config.ContainerLabels{
			AppName:      appName,
			DeploymentID: deploymentID,
			Port:         config.Port(constants.DefaultContainerPort),
			Domains: []config.Domain{
				{Canonical: appName + ".example.com"},
			},
		},
		IP:   "10.0.0.1",
		Port: "8080",
	}
}

func TestShouldCleanupOldContainers(t *testing.T) {
	const (
		oldID = "001"
		newID = "002"
	)

	deploymentsWith := func(deploymentID string) map[string]Deployment {
		dm := NewDeploymentManager(nil, nil)
		dm.UpdateDeployments([]HealthyContainer{healthyContainerFor("myapp", deploymentID, "c1")})
		return dm.Deployments()
	}

	tests := []struct {
		name        string
		deployments map[string]Deployment
		app         *TriggeredByApp
		want        bool
	}{
		{
			name:        "new deployment is healthy and current",
			deployments: deploymentsWith(newID),
			app:         &TriggeredByApp{appName: "myapp", deploymentID: newID, startedSelected: true},
			want:        true,
		},
		{
			name:        "stale batch from the old container's stop events",
			deployments: deploymentsWith(newID),
			app:         &TriggeredByApp{appName: "myapp", deploymentID: oldID},
			want:        false,
		},
		{
			name:        "new deployment failed while the old one is still healthy",
			deployments: deploymentsWith(oldID),
			app:         &TriggeredByApp{appName: "myapp", deploymentID: newID, startedSelected: true},
			want:        false,
		},
		{
			name:        "app has no healthy deployment",
			deployments: map[string]Deployment{},
			app:         &TriggeredByApp{appName: "myapp", deploymentID: newID, startedSelected: true},
			want:        false,
		},
		{
			name:        "no triggering app",
			deployments: deploymentsWith(newID),
			app:         nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCleanupOldContainers(tt.deployments, tt.app); got != tt.want {
				t.Fatalf("shouldCleanupOldContainers() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Regression test for https://github.com/haloydev/haloy/issues/27: after a
// rolling deploy, the old container's stop events trigger an update carrying
// the old deployment id. When another app changes in the same window the
// update no longer returns early, and the cleanup must not treat the old id
// as the one to keep.
func TestStaleBatchDoesNotCleanupCurrentDeployment(t *testing.T) {
	const (
		oldID = "001"
		newID = "002"
	)

	dm := NewDeploymentManager(nil, nil)

	dm.UpdateDeployments([]HealthyContainer{
		healthyContainerFor("app-a", oldID, "a-old"),
		healthyContainerFor("app-b", "01jd00000000000000000000b1", "b1"),
	})

	// App A is deployed: the new container is healthy next to the old one.
	dm.UpdateDeployments([]HealthyContainer{
		healthyContainerFor("app-a", oldID, "a-old"),
		healthyContainerFor("app-a", newID, "a-new"),
		healthyContainerFor("app-b", "01jd00000000000000000000b1", "b1"),
	})

	// The old container of A is gone and B was stopped in the meantime, so
	// the update triggered by the old container's stop events sees a change.
	if changed := dm.UpdateDeployments([]HealthyContainer{
		healthyContainerFor("app-a", newID, "a-new"),
	}); !changed {
		t.Fatal("expected removing app-b to register as a deployment change")
	}

	deployments := dm.Deployments()

	stale := &TriggeredByApp{appName: "app-a", deploymentID: oldID}
	if shouldCleanupOldContainers(deployments, stale) {
		t.Fatal("update carrying the old deployment id must not clean up app-a's containers")
	}

	current := &TriggeredByApp{appName: "app-a", deploymentID: newID, startedSelected: true}
	if !shouldCleanupOldContainers(deployments, current) {
		t.Fatal("update carrying the current deployment id should clean up older containers")
	}
}
