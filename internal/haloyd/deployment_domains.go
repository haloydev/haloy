package haloyd

import "strings"

func deploymentDomainSet(deployments map[string]Deployment) map[string]struct{} {
	domains := make(map[string]struct{})
	for _, deployment := range deployments {
		addDeploymentDomains(domains, deployment)
	}
	return domains
}

func addDeploymentDomains(domains map[string]struct{}, deployment Deployment) {
	if deployment.Labels == nil {
		return
	}

	for _, domain := range deployment.Labels.Domains {
		addDomain(domains, domain.Canonical)
		for _, alias := range domain.Aliases {
			addDomain(domains, alias)
		}
	}
}

func deploymentOverlapsDomains(deployment Deployment, domains map[string]struct{}) bool {
	if deployment.Labels == nil {
		return false
	}

	for _, domain := range deployment.Labels.Domains {
		if domainInSet(domain.Canonical, domains) {
			return true
		}
		for _, alias := range domain.Aliases {
			if domainInSet(alias, domains) {
				return true
			}
		}
	}

	return false
}

func addDomain(domains map[string]struct{}, domain string) {
	domain = strings.ToLower(domain)
	if domain == "" {
		return
	}
	domains[domain] = struct{}{}
}

func domainInSet(domain string, domains map[string]struct{}) bool {
	_, exists := domains[strings.ToLower(domain)]
	return exists
}
