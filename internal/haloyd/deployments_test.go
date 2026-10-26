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

// A container behind another app's proxy owns no domain of its own. It still
// has to be tracked, or Update() never sees the app change and never reaches
// the step that stops the previous deployment's containers.
func TestDomainlessDeploymentIsTrackedButNotRouted(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)

	instance := func(deploymentID, containerID, ip string) []HealthyContainer {
		return []HealthyContainer{
			{
				ContainerID: containerID,
				Labels: &config.ContainerLabels{
					AppName:      "gated-app",
					DeploymentID: deploymentID,
					Port:         config.Port(constants.DefaultContainerPort),
				},
				IP:   ip,
				Port: "4000",
			},
		}
	}

	if !dm.UpdateDeployments(instance("deploy-1", "c1", "10.0.0.1")) {
		t.Fatal("first domainless deployment should register as a change")
	}
	if _, ok := dm.Deployments()["gated-app"]; !ok {
		t.Fatal("expected gated-app to be tracked")
	}

	if !dm.UpdateDeployments(instance("deploy-2", "c2", "10.0.0.2")) {
		t.Fatal("rolling to a new deployment ID should register as a change")
	}

	snapshot := buildSnapshot(dm.Deployments(), dm.FailedDeployments(), "", nil)
	if len(snapshot.Routes) != 0 {
		t.Fatalf("domainless deployment should produce no routes, got %d", len(snapshot.Routes))
	}

	// Losing every container leaves nothing to keep a 502 route for.
	dm.UpdateDeployments(nil)
	if len(dm.FailedDeployments()) != 0 {
		t.Fatalf("expected no failed deployments, got %d", len(dm.FailedDeployments()))
	}
}

// The health monitor only exists to pull unhealthy backends out of the proxy.
// A domainless app has none there, and probing it would mean sending HTTP at a
// database that never agreed to answer.
func TestHealthCheckTargetsSkipDomainlessDeployments(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)

	dm.UpdateDeployments([]HealthyContainer{
		{
			ContainerID: "db",
			Labels: &config.ContainerLabels{
				AppName:      "postgres",
				DeploymentID: "deploy-1",
				Port:         config.Port("5432"),
			},
			IP:   "10.0.0.1",
			Port: "5432",
		},
		{
			ContainerID: "web",
			Labels: &config.ContainerLabels{
				AppName:      "web",
				DeploymentID: "deploy-1",
				Port:         config.Port(constants.DefaultContainerPort),
				Domains:      []config.Domain{{Canonical: "web.example.com"}},
			},
			IP:   "10.0.0.2",
			Port: "8080",
		},
	})

	targets := dm.GetHealthCheckTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 health check target, got %d", len(targets))
	}
	if targets[0].AppName != "web" {
		t.Fatalf("expected only the routed app to be monitored, got %s", targets[0].AppName)
	}
}
