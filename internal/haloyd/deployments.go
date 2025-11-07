package haloyd

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/docker/docker/client"
)

type DeploymentInstance struct {
	ContainerID string
	IP          string
	Port        string
}

type Deployment struct {
	Labels    *config.ContainerLabels
	Instances []DeploymentInstance
}

type ContainerExclusionReason int

const (
	ExclusionReasonInspectionFailed ContainerExclusionReason = iota
	ExclusionReasonLabelParsingFailed
	ExclusionReasonNoDomains
	ExclusionReasonNotDefaultNetwork
	ExclusionReasonIPExtractionFailed
)

func (r ContainerExclusionReason) String() string {
	switch r {
	case ExclusionReasonInspectionFailed:
		return "container inspection failed"
	case ExclusionReasonLabelParsingFailed:
		return "label parsing failed"
	case ExclusionReasonNoDomains:
		return "no domains configured"
	case ExclusionReasonNotDefaultNetwork:
		return "not on haloy docker network"
	case ExclusionReasonIPExtractionFailed:
		return "IP extraction failed"
	default:
		return "unknown reason"
	}
}

type ExcludedContainerInfo struct {
	ContainerID string
	Reason      ContainerExclusionReason
	Error       string
	Labels      *config.ContainerLabels
}

type DeploymentManager struct {
	cli *client.Client
	// deployments is a map of appName to Deployment, key is the app name.
	deployments      map[string]Deployment
	compareResult    compareResult
	deploymentsMutex sync.RWMutex
	haloydConfig     *config.HaloydConfig
}

func NewDeploymentManager(cli *client.Client, haloydConfig *config.HaloydConfig) *DeploymentManager {
	return &DeploymentManager{
		cli:          cli,
		deployments:  make(map[string]Deployment),
		haloydConfig: haloydConfig,
	}
}

// BuildDeployments gets running Docker containers with the app label and builds a map of current deployments in the system.
// It compares the new deployment state with the previous state to determine if any changes have occurred (additions, removals, or updates to deployments).
// Returns true if the deployment state has changed, along with any error encountered.
func (dm *DeploymentManager) BuildDeployments(ctx context.Context, logger *slog.Logger) (hasChanged bool, excludedContainers []ExcludedContainerInfo, err error) {
	newDeployments := make(map[string]Deployment)
	containers, err := docker.GetAppContainers(ctx, dm.cli, false, "")
	if err != nil {
		return hasChanged, excludedContainers, fmt.Errorf("failed to get containers: %w", err)
	}

	for _, containerSummary := range containers {
		container, err := dm.cli.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			logger.Error("Failed to inspect container", "container_id", containerSummary.ID, "error", err)
			excludedContainers = append(excludedContainers,
				ExcludedContainerInfo{
					ContainerID: containerSummary.ID,
					Reason:      ExclusionReasonInspectionFailed,
					Error:       err.Error(),
					Labels:      nil,
				})
			continue
		}

		labels, err := config.ParseContainerLabels(container.Config.Labels)
		if err != nil {
			logger.Error("Error parsing labels for container", "container_id", container.ID, "error", err)
			excludedContainers = append(excludedContainers,
				ExcludedContainerInfo{
					ContainerID: container.ID,
					Reason:      ExclusionReasonLabelParsingFailed,
					Error:       err.Error(),
					Labels:      nil,
				})
			continue
		}

		_, isOnNetwork := container.NetworkSettings.Networks[constants.DockerNetwork]
		if !isOnNetwork {
			excludedContainers = append(excludedContainers, ExcludedContainerInfo{
				ContainerID: container.ID,
				Reason:      ExclusionReasonNotDefaultNetwork,
				Error:       "",
				Labels:      labels,
			})
			continue
		}

		if len(labels.Domains) == 0 {
			excludedContainers = append(excludedContainers, ExcludedContainerInfo{
				ContainerID: container.ID,
				Reason:      ExclusionReasonNoDomains,
				Error:       "",
				Labels:      labels,
			})
			continue
		}

		ip, err := docker.ContainerNetworkIP(container, constants.DockerNetwork)
		if err != nil {
			logger.Error("Error getting IP for container", "container_id", helpers.SafeIDPrefix(container.ID), "error", err)
			excludedContainers = append(excludedContainers, ExcludedContainerInfo{
				ContainerID: container.ID,
				Reason:      ExclusionReasonIPExtractionFailed,
				Error:       err.Error(),
				Labels:      labels,
			})
			continue
		}

		var port string
		if labels.Port != "" {
			port = labels.Port.String()
		} else {
			port = constants.DefaultContainerPort
		}

		instance := DeploymentInstance{ContainerID: container.ID, IP: ip, Port: port}

		if deployment, exists := newDeployments[labels.AppName]; exists {
			// There is a appName match, check if the deployment ID matches.
			if deployment.Labels.DeploymentID == labels.DeploymentID {
				deployment.Instances = append(deployment.Instances, instance)
				newDeployments[labels.AppName] = deployment
			} else {
				// Replace the deployment if the new one has a higher deployment ID
				if deployment.Labels.DeploymentID < labels.DeploymentID {
					newDeployments[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
				}
			}
		} else {
			newDeployments[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
		}
	}

	dm.deploymentsMutex.Lock()
	defer dm.deploymentsMutex.Unlock()

	oldDeployments := dm.deployments
	dm.deployments = newDeployments

	compareResult := compareDeployments(oldDeployments, newDeployments)
	hasChanged = len(compareResult.AddedDeployments) > 0 ||
		len(compareResult.RemovedDeployments) > 0 ||
		len(compareResult.UpdatedDeployments) > 0

	dm.compareResult = compareResult
	return hasChanged, excludedContainers, nil
}

func (dm *DeploymentManager) HealthCheckNewContainers(ctx context.Context, logger *slog.Logger) (checked []Deployment, failedContainerIDs []string) {
	for _, deployment := range dm.compareResult.AddedDeployments {
		checked = append(checked, deployment)
	}

	for _, deployment := range dm.compareResult.UpdatedDeployments {
		checked = append(checked, deployment)
	}

	for _, deployment := range checked {
		for _, instance := range deployment.Instances {
			if err := docker.HealthCheckContainer(ctx, dm.cli, logger, instance.ContainerID); err != nil {
				failedContainerIDs = append(failedContainerIDs, instance.ContainerID)
			}
		}
	}
	return checked, failedContainerIDs
}

func (dm *DeploymentManager) Deployments() map[string]Deployment {
	dm.deploymentsMutex.RLock()
	defer dm.deploymentsMutex.RUnlock()

	// Return a copy to prevent external modification after unlock
	deploymentsCopy := make(map[string]Deployment, len(dm.deployments))
	maps.Copy(deploymentsCopy, dm.deployments)
	return deploymentsCopy
}

// GetCertificateDomains collects all canonical domains and their aliases for certificate management.
func (dm *DeploymentManager) GetCertificateDomains() ([]CertificatesDomain, error) {
	dm.deploymentsMutex.RLock()
	defer dm.deploymentsMutex.RUnlock()

	certDomains := make([]CertificatesDomain, 0, len(dm.deployments))

	for _, deployment := range dm.deployments {
		if deployment.Labels == nil {
			continue
		}
		for _, domain := range deployment.Labels.Domains {
			if domain.Canonical != "" {
				email := deployment.Labels.ACMEEmail
				if dm.haloydConfig != nil && email == "" {
					email = dm.haloydConfig.Certificates.AcmeEmail // Use default email if not set
				}

				if email == "" {
					return nil, fmt.Errorf("ACME email for domain %s not found in haloyd config or labels", domain.Canonical)
				}

				newDomain := CertificatesDomain{
					Canonical: domain.Canonical,
					Aliases:   domain.Aliases,
					Email:     email,
				}

				if err := newDomain.Validate(); err != nil {
					return nil, fmt.Errorf("domain not valid '%s': %w", domain.Canonical, err)
				}

				certDomains = append(certDomains, newDomain)
			}
		}
	}

	// We'll add the domain set in the haloyd config file if it exists.
	if dm.haloydConfig != nil && dm.haloydConfig.API.Domain != "" && dm.haloydConfig.Certificates.AcmeEmail != "" {
		apiDomain := CertificatesDomain{
			Canonical: dm.haloydConfig.API.Domain,
			Aliases:   []string{},
			Email:     dm.haloydConfig.Certificates.AcmeEmail,
		}
		certDomains = append(certDomains, apiDomain)
	}
	return certDomains, nil
}

type compareResult struct {
	UpdatedDeployments map[string]Deployment
	RemovedDeployments map[string]Deployment
	AddedDeployments   map[string]Deployment
}

// compareDeployments analyzes differences between the previous and current deployment states.
// It identifies three types of changes:
// 1. Updated deployments - same app name but different deployment ID or instance configuration
// 2. Removed deployments - deployments that existed before but are no longer present
// 3. Added deployments - new deployments that didn't exist in the previous state
func compareDeployments(oldDeployments, newDeployments map[string]Deployment) compareResult {
	updatedDeployments := make(map[string]Deployment)
	removedDeployments := make(map[string]Deployment)
	addedDeployments := make(map[string]Deployment)

	for appName, prevDeployment := range oldDeployments {
		if currentDeployment, exists := newDeployments[appName]; exists {
			if prevDeployment.Labels.DeploymentID != currentDeployment.Labels.DeploymentID {
				updatedDeployments[appName] = currentDeployment
			} else {
				if !instancesEqual(prevDeployment.Instances, currentDeployment.Instances) {
					updatedDeployments[appName] = currentDeployment
				}
			}
		} else {
			removedDeployments[appName] = prevDeployment
		}
	}

	for appName, currentDeployment := range newDeployments {
		if _, exists := oldDeployments[appName]; !exists {
			addedDeployments[appName] = currentDeployment
		}
	}

	result := compareResult{
		UpdatedDeployments: updatedDeployments,
		RemovedDeployments: removedDeployments,
		AddedDeployments:   addedDeployments,
	}

	return result
}

func instancesEqual(a, b []DeploymentInstance) bool {
	if len(a) != len(b) {
		return false
	}

	mapA := make(map[string]bool)
	for _, instance := range a {
		mapA[instance.ContainerID] = true
	}

	for _, instance := range b {
		if !mapA[instance.ContainerID] {
			return false
		}
	}

	return true
}
