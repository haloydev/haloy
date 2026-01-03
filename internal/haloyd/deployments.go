package haloyd

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
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

// DiscoveredContainer represents a container found with haloy labels
// but not yet validated as healthy/routable.
type DiscoveredContainer struct {
	ContainerID   string
	Labels        *config.ContainerLabels
	ContainerInfo container.InspectResponse
	Port          string
}

// HealthyContainer is a container that passed health checks and is ready to receive traffic.
type HealthyContainer struct {
	ContainerID string
	Labels      *config.ContainerLabels
	IP          string
	Port        string
}

// FailedContainer represents a container that failed discovery or health check.
type FailedContainer struct {
	ContainerID string
	Labels      *config.ContainerLabels // May be nil if label parsing failed
	Reason      string                  // Human-readable failure reason
	Err         error                   // Underlying error
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

// DiscoverContainers finds all containers with haloy labels and validates their basic configuration.
// It returns containers that are eligible for health checking, and containers that failed validation.
func (dm *DeploymentManager) DiscoverContainers(ctx context.Context, logger *slog.Logger) (discovered []DiscoveredContainer, failed []FailedContainer, err error) {
	containers, err := docker.GetAppContainers(ctx, dm.cli, false, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get containers: %w", err)
	}

	for _, containerSummary := range containers {
		containerInfo, err := dm.cli.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			logger.Debug("Failed to inspect container", "container_id", containerSummary.ID, "error", err)
			failed = append(failed, FailedContainer{
				ContainerID: containerSummary.ID,
				Labels:      nil,
				Reason:      "container inspection failed",
				Err:         err,
			})
			continue
		}

		labels, err := config.ParseContainerLabels(containerInfo.Config.Labels)
		if err != nil {
			logger.Debug("Error parsing labels for container", "container_id", containerInfo.ID, "error", err)
			failed = append(failed, FailedContainer{
				ContainerID: containerInfo.ID,
				Labels:      nil,
				Reason:      "label parsing failed",
				Err:         err,
			})
			continue
		}

		// Check if container is on the haloy network
		_, isOnNetwork := containerInfo.NetworkSettings.Networks[constants.DockerNetwork]
		if !isOnNetwork {
			logger.Debug("Container not on haloy network, skipping",
				"container_id", helpers.SafeIDPrefix(containerInfo.ID),
				"app", labels.AppName)
			continue
		}

		// Validate port configuration
		labelPortString := labels.Port.String()
		if !validateContainerPort(containerInfo.Config.ExposedPorts, labelPortString) {
			exposedPortsStr := exposedPortsAsString(containerInfo.Config.ExposedPorts)
			failed = append(failed, FailedContainer{
				ContainerID: containerInfo.ID,
				Labels:      labels,
				Reason:      "port mismatch",
				Err:         fmt.Errorf("configured port %s does not match exposed ports %s", labelPortString, exposedPortsStr),
			})
			continue
		}

		// Check domains are configured
		if len(labels.Domains) == 0 {
			logger.Debug("Container has no domains configured, skipping",
				"container_id", helpers.SafeIDPrefix(containerInfo.ID),
				"app", labels.AppName)
			continue
		}

		// Determine port
		var port string
		if labels.Port != "" {
			port = labels.Port.String()
		} else {
			port = constants.DefaultContainerPort
		}

		discovered = append(discovered, DiscoveredContainer{
			ContainerID:   containerInfo.ID,
			Labels:        labels,
			ContainerInfo: containerInfo,
			Port:          port,
		})
	}

	return discovered, failed, nil
}

// HealthCheckContainers performs health checks on all discovered containers.
// Returns healthy containers (with IPs) and failed containers with detailed error information.
func (dm *DeploymentManager) HealthCheckContainers(ctx context.Context, logger *slog.Logger, discovered []DiscoveredContainer) (healthy []HealthyContainer, failed []FailedContainer) {
	for _, container := range discovered {
		result := docker.HealthCheckContainer(ctx, dm.cli, logger, container.ContainerID, container.ContainerInfo)
		if result.Err != nil {
			logger.Debug("Container failed health check",
				"container_id", helpers.SafeIDPrefix(container.ContainerID),
				"app", container.Labels.AppName,
				"error", result.Err)
			failed = append(failed, FailedContainer{
				ContainerID: container.ContainerID,
				Labels:      container.Labels,
				Reason:      "health check failed",
				Err:         result.Err,
			})
			continue
		}

		healthy = append(healthy, HealthyContainer{
			ContainerID: container.ContainerID,
			Labels:      container.Labels,
			IP:          result.IP,
			Port:        container.Port,
		})
	}

	return healthy, failed
}

// UpdateDeployments builds the deployment map from healthy containers and compares with previous state.
// Returns whether the deployment state has changed.
func (dm *DeploymentManager) UpdateDeployments(healthy []HealthyContainer) (hasChanged bool) {
	newDeployments := make(map[string]Deployment)

	for _, container := range healthy {
		instance := DeploymentInstance{
			ContainerID: container.ContainerID,
			IP:          container.IP,
			Port:        container.Port,
		}

		if deployment, exists := newDeployments[container.Labels.AppName]; exists {
			// There is an appName match, check if the deployment ID matches.
			if deployment.Labels.DeploymentID == container.Labels.DeploymentID {
				deployment.Instances = append(deployment.Instances, instance)
				newDeployments[container.Labels.AppName] = deployment
			} else {
				// Replace the deployment if the new one has a higher deployment ID
				if deployment.Labels.DeploymentID < container.Labels.DeploymentID {
					newDeployments[container.Labels.AppName] = Deployment{
						Labels:    container.Labels,
						Instances: []DeploymentInstance{instance},
					}
				}
			}
		} else {
			newDeployments[container.Labels.AppName] = Deployment{
				Labels:    container.Labels,
				Instances: []DeploymentInstance{instance},
			}
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
	return hasChanged
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

// validateContainerPort checks if the port specified in labels matches any exposed port on the container
func validateContainerPort(exposedPorts nat.PortSet, labelPort string) bool {
	if labelPort == "" {
		labelPort = constants.DefaultContainerPort
	}

	// If no ports are exposed, we cannot validate but assume it's valid
	// The health check will catch actual connectivity issues
	if len(exposedPorts) == 0 {
		return true
	}

	// Check if the label port exists in the exposed ports
	for exposedPort := range exposedPorts {
		if exposedPort.Port() == labelPort {
			return true
		}
	}

	return false
}

// exposedPortsAsString returns a string representation of exposed ports for logging
func exposedPortsAsString(exposedPorts nat.PortSet) string {
	if len(exposedPorts) == 0 {
		return "none"
	}

	ports := make([]string, 0, len(exposedPorts))
	for port := range exposedPorts {
		ports = append(ports, port.Port())
	}

	return fmt.Sprintf("[%s]", strings.Join(ports, ", "))
}
