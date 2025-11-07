package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
)

const (
	LabelAppName         = "dev.haloy.appName"
	LabelDeploymentID    = "dev.haloy.deployment-id"
	LabelHealthCheckPath = "dev.haloy.health-check-path" // optional default to "/"
	LabelACMEEmail       = "dev.haloy.acme.email"
	LabelPort            = "dev.haloy.port" // optional

	// Format strings for indexed canonical domains and aliases.
	// Use fmt.Sprintf(LabelDomainCanonical, index) to get "dev.haloy.domain.<index>"
	LabelDomainCanonical = "dev.haloy.domain.%d"
	// Use fmt.Sprintf(LabelDomainAlias, domainIndex, aliasIndex) to get "dev.haloy.domain.<domainIndex>.alias.<aliasIndex>"
	LabelDomainAlias = "dev.haloy.domain.%d.alias.%d"
	// Used to identify the role of the container (e.g., "haproxy", "haloyd", etc.)
	LabelRole = "dev.haloy.role"
)

const (
	HAProxyLabelRole = "haproxy"
	HaloydLabelRole  = "haloyd"
	AppLabelRole     = "app"
)

type ContainerLabels struct {
	AppName         string
	DeploymentID    string
	HealthCheckPath string
	ACMEEmail       string
	Port            Port
	Domains         []Domain
	Role            string
}

// Parse from docker labels to ContainerLabels struct.
func ParseContainerLabels(labels map[string]string) (*ContainerLabels, error) {
	cl := &ContainerLabels{
		AppName:      labels[LabelAppName],
		DeploymentID: labels[LabelDeploymentID],
		ACMEEmail:    labels[LabelACMEEmail],
		Role:         labels[LabelRole],
	}

	if v, ok := labels[LabelPort]; ok {
		cl.Port = Port(v)
	} else {
		cl.Port = constants.DefaultContainerPort
	}

	// Set HealthCheckPath with default value.
	if v, ok := labels[LabelHealthCheckPath]; ok {
		cl.HealthCheckPath = v
	} else {
		cl.HealthCheckPath = constants.DefaultHealthCheckPath
	}

	// Parse domains
	domainMap := make(map[int]*Domain)

	// Process domain and alias labels.
	for key, value := range labels {
		if !strings.HasPrefix(key, "dev.haloy.domain.") {
			continue
		}
		if strings.Contains(key, ".alias.") {
			// Parse alias key: "dev.haloy.domain.<domainIdx>.alias.<aliasIdx>"
			var domainIdx, aliasIdx int
			if _, err := fmt.Sscanf(key, LabelDomainAlias, &domainIdx, &aliasIdx); err != nil {
				// Skip keys that don't conform.
				continue
			}
			domain := getOrCreateDomain(domainMap, domainIdx)
			domain.Aliases = append(domain.Aliases, value)
		} else {
			// Parse canonical domain key: "dev.haloy.domain.<domainIdx>"
			var domainIdx int
			if _, err := fmt.Sscanf(key, LabelDomainCanonical, &domainIdx); err != nil {
				continue
			}
			domain := getOrCreateDomain(domainMap, domainIdx)
			domain.Canonical = value
		}
	}

	// Build the sorted slice of domains.
	var indices []int
	for i := range domainMap {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		cl.Domains = append(cl.Domains, *domainMap[i])
	}

	// Validate the parsed labels.
	if err := cl.Validate(); err != nil {
		return nil, err
	}

	return cl, nil
}

// getOrCreateDomain returns an existing *config.Domain from domainMap or creates a new one.
func getOrCreateDomain(domainMap map[int]*Domain, idx int) *Domain {
	if domain, exists := domainMap[idx]; exists {
		return domain
	}
	domainMap[idx] = &Domain{}
	return domainMap[idx]
}

// ToLabels converts the ContainerLabels struct back to a map[string]string.
func (cl *ContainerLabels) ToLabels() map[string]string {
	labels := map[string]string{
		LabelAppName:         cl.AppName,
		LabelDeploymentID:    cl.DeploymentID,
		LabelHealthCheckPath: cl.HealthCheckPath,
		LabelPort:            cl.Port.String(),
		LabelACMEEmail:       cl.ACMEEmail,
		LabelRole:            cl.Role,
	}

	// Iterate through the domains slice.
	for i, domain := range cl.Domains {
		// Set canonical domain.
		canonicalKey := fmt.Sprintf(LabelDomainCanonical, i)
		labels[canonicalKey] = domain.Canonical

		// Set aliases.
		for j, alias := range domain.Aliases {
			aliasKey := fmt.Sprintf(LabelDomainAlias, i, j)
			labels[aliasKey] = alias
		}
	}

	return labels
}

// We assume that all labels need to be present for the labels to be valid.
func (cl *ContainerLabels) Validate() error {
	if cl.AppName == "" {
		return fmt.Errorf("appName is required")
	}
	if cl.DeploymentID == "" {
		return fmt.Errorf("deploymentID is required")
	}

	if len(cl.Domains) > 0 {
		for _, domain := range cl.Domains {
			if err := domain.Validate(); err != nil {
				return fmt.Errorf("domain validation failed: %w", err)
			}
		}
	}

	if cl.ACMEEmail != "" && !helpers.IsValidEmail(cl.ACMEEmail) {
		return fmt.Errorf("ACME email is not valid")
	}

	if cl.Port == "" {
		return fmt.Errorf("port is required")
	}

	if cl.Role != AppLabelRole {
		return fmt.Errorf("role must be '%s'", AppLabelRole)
	}

	return nil
}
