package haloyd

import (
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

func TestUpdateDeploymentsTracksFailedDeployments(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)

	labels := &config.ContainerLabels{
		AppName:      "myapp",
		DeploymentID: "deploy-1",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "myapp.example.com"},
		},
	}

	healthy := []HealthyContainer{
		{
			ContainerID: "c1",
			Labels:      labels,
			IP:          "10.0.0.1",
			Port:        "8080",
		},
	}

	dm.UpdateDeployments(healthy)

	if len(dm.FailedDeployments()) != 0 {
		t.Fatal("expected no failed deployments initially")
	}

	// All containers die
	dm.UpdateDeployments(nil)

	failed := dm.FailedDeployments()
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed deployment, got %d", len(failed))
	}
	if _, ok := failed["myapp"]; !ok {
		t.Fatal("expected myapp in failed deployments")
	}
	if failed["myapp"].Labels.DeploymentID != "deploy-1" {
		t.Fatalf("expected deployment ID deploy-1, got %s", failed["myapp"].Labels.DeploymentID)
	}
}

func TestFailedDeploymentsClearedOnRedeploy(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)

	labels := &config.ContainerLabels{
		AppName:      "myapp",
		DeploymentID: "deploy-1",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "myapp.example.com"},
		},
	}

	healthy := []HealthyContainer{
		{
			ContainerID: "c1",
			Labels:      labels,
			IP:          "10.0.0.1",
			Port:        "8080",
		},
	}

	dm.UpdateDeployments(healthy)
	dm.UpdateDeployments(nil)

	if len(dm.FailedDeployments()) != 1 {
		t.Fatal("expected 1 failed deployment after container death")
	}

	// Re-deploy with new deployment ID
	newLabels := &config.ContainerLabels{
		AppName:      "myapp",
		DeploymentID: "deploy-2",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "myapp.example.com"},
		},
	}

	redeployed := []HealthyContainer{
		{
			ContainerID: "c2",
			Labels:      newLabels,
			IP:          "10.0.0.2",
			Port:        "8080",
		},
	}

	dm.UpdateDeployments(redeployed)

	if len(dm.FailedDeployments()) != 0 {
		t.Fatal("expected failed deployments to be cleared after successful re-deploy")
	}

	deployments := dm.Deployments()
	if len(deployments) != 1 {
		t.Fatalf("expected 1 active deployment, got %d", len(deployments))
	}
	if deployments["myapp"].Labels.DeploymentID != "deploy-2" {
		t.Fatalf("expected deployment ID deploy-2, got %s", deployments["myapp"].Labels.DeploymentID)
	}
}

func TestFailedDeploymentClearedWhenRenamedAppReusesDomain(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)

	oldLabels := &config.ContainerLabels{
		AppName:      "old-app",
		DeploymentID: "deploy-1",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "app.example.com"},
		},
	}

	dm.UpdateDeployments([]HealthyContainer{
		{
			ContainerID: "c1",
			Labels:      oldLabels,
			IP:          "10.0.0.1",
			Port:        "8080",
		},
	})
	dm.UpdateDeployments(nil)

	if _, ok := dm.FailedDeployments()["old-app"]; !ok {
		t.Fatal("expected old-app to be tracked as failed after removal")
	}

	newLabels := &config.ContainerLabels{
		AppName:      "new-app",
		DeploymentID: "deploy-2",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "app.example.com"},
		},
	}

	dm.UpdateDeployments([]HealthyContainer{
		{
			ContainerID: "c2",
			Labels:      newLabels,
			IP:          "10.0.0.2",
			Port:        "8080",
		},
	})

	if len(dm.FailedDeployments()) != 0 {
		t.Fatalf("expected failed deployment to be cleared after renamed app reused domain, got %d", len(dm.FailedDeployments()))
	}

	deployments := dm.Deployments()
	if _, ok := deployments["new-app"]; !ok {
		t.Fatal("expected new-app to be active")
	}
}

func TestRemovedDeploymentNotTrackedAsFailedWhenReplacementUsesAlias(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)

	oldLabels := &config.ContainerLabels{
		AppName:      "old-app",
		DeploymentID: "deploy-1",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "app.example.com", Aliases: []string{"www.example.com"}},
		},
	}

	dm.UpdateDeployments([]HealthyContainer{
		{
			ContainerID: "c1",
			Labels:      oldLabels,
			IP:          "10.0.0.1",
			Port:        "8080",
		},
	})

	newLabels := &config.ContainerLabels{
		AppName:      "new-app",
		DeploymentID: "deploy-2",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "www.example.com"},
		},
	}

	dm.UpdateDeployments([]HealthyContainer{
		{
			ContainerID: "c2",
			Labels:      newLabels,
			IP:          "10.0.0.2",
			Port:        "8080",
		},
	})

	if len(dm.FailedDeployments()) != 0 {
		t.Fatalf("expected removed old-app not to be tracked as failed when new-app owns an overlapping alias, got %d", len(dm.FailedDeployments()))
	}
}
