package haloyd

import (
	"slices"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxywire"
)

// buildSnapshot converts deployments into a proxy routing snapshot.
// includeInstance filters which instances become backends; nil includes all.
// Apps in failedDeployments that are no longer deployed keep their routes with
// no backends, so the proxy serves 502 instead of 404 for them. Validation of
// domain collisions happens when the snapshot is converted to a proxy config.
func buildSnapshot(
	deployments map[string]Deployment,
	failedDeployments map[string]Deployment,
	apiDomain string,
	includeInstance func(DeploymentInstance) bool,
) *proxywire.Snapshot {
	var routes []proxywire.Route

	for _, d := range deployments {
		var backends []proxywire.Backend
		for _, inst := range d.Instances {
			if includeInstance != nil && !includeInstance(inst) {
				continue
			}
			backends = append(backends, proxywire.Backend{IP: inst.IP, Port: inst.Port})
		}

		for _, domain := range d.Labels.Domains {
			if domain.Canonical == "" {
				continue
			}
			routes = append(routes, proxywire.Route{
				Canonical: domain.Canonical,
				Aliases:   domain.Aliases,
				Backends:  backends,
			})
		}
	}

	for appName, d := range failedDeployments {
		if _, exists := deployments[appName]; exists {
			continue
		}

		for _, domain := range d.Labels.Domains {
			if domain.Canonical == "" {
				continue
			}
			routes = append(routes, proxywire.Route{
				Canonical: domain.Canonical,
				Aliases:   domain.Aliases,
			})
		}
	}

	// Deterministic order keeps the snapshot file diff-friendly.
	slices.SortFunc(routes, func(a, b proxywire.Route) int {
		return strings.Compare(a.Canonical, b.Canonical)
	})

	return &proxywire.Snapshot{
		SchemaVersion: proxywire.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		APIDomain:     apiDomain,
		APIBackend:    &proxywire.Backend{IP: constants.HaloydAPIHost, Port: constants.HaloydAPIPort},
		Routes:        routes,
	}
}
