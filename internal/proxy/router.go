package proxy

import (
	"fmt"
	"strings"
)

// RouteBuilder helps build proxy routes from deployment information.
type RouteBuilder struct {
	routes    map[string]*Route
	apiDomain string
}

// NewRouteBuilder creates a new route builder.
func NewRouteBuilder() *RouteBuilder {
	return &RouteBuilder{
		routes: make(map[string]*Route),
	}
}

// SetAPIDomain sets the API domain for the configuration.
func (rb *RouteBuilder) SetAPIDomain(domain string) {
	rb.apiDomain = strings.ToLower(domain)
}

// AddRoute adds a route for an application.
func (rb *RouteBuilder) AddRoute(canonical string, aliases []string, backends []Backend) {
	canonical = strings.ToLower(canonical)

	route := &Route{
		Canonical: canonical,
		Aliases:   make([]string, len(aliases)),
		Backends:  backends,
	}

	for i, alias := range aliases {
		route.Aliases[i] = strings.ToLower(alias)
	}

	rb.routes[canonical] = route
}

// Build creates the final proxy configuration.
// Returns an error if duplicate aliases are detected across routes.
func (rb *RouteBuilder) Build() (*Config, error) {
	// Check for duplicate aliases across all routes
	aliasOwner := make(map[string]string) // alias -> canonical that owns it

	for canonical, route := range rb.routes {
		// Check if canonical conflicts with another route's alias
		if owner, exists := aliasOwner[canonical]; exists {
			return nil, fmt.Errorf("domain %q is both a canonical domain and an alias of %q", canonical, owner)
		}

		for _, alias := range route.Aliases {
			if owner, exists := aliasOwner[alias]; exists {
				return nil, fmt.Errorf("alias %q is used by both %q and %q", alias, owner, canonical)
			}
			aliasOwner[alias] = canonical
		}

		// Also track canonical to detect alias→canonical conflicts
		aliasOwner[canonical] = canonical
	}

	return &Config{
		Routes:    rb.routes,
		APIDomain: rb.apiDomain,
	}, nil
}

// BuildRoutesFromDeployments is a helper function to build routes from a deployment map.
// This function bridges the gap between the deployment manager's data structure and the proxy's route configuration.
type DeploymentInfo struct {
	AppName   string
	Canonical string
	Aliases   []string
	Instances []InstanceInfo
}

type InstanceInfo struct {
	IP   string
	Port string
}

// BuildConfigFromDeployments creates a proxy config from deployment information.
func BuildConfigFromDeployments(deployments []DeploymentInfo, apiDomain string) (*Config, error) {
	rb := NewRouteBuilder()
	rb.SetAPIDomain(apiDomain)

	for _, d := range deployments {
		if d.Canonical == "" {
			continue
		}

		backends := make([]Backend, 0, len(d.Instances))
		for _, inst := range d.Instances {
			backends = append(backends, Backend{
				IP:   inst.IP,
				Port: inst.Port,
			})
		}

		rb.AddRoute(d.Canonical, d.Aliases, backends)
	}

	return rb.Build()
}

// ConvertDeployments is a convenience type alias for the deployment map format
// used by haloyd's DeploymentManager.
type HaloydDeployment struct {
	AppName   string
	Domains   []HaloydDomain
	Instances []HaloydInstance
}

type HaloydDomain struct {
	Canonical string
	Aliases   []string
}

type HaloydInstance struct {
	IP   string
	Port string
}

// BuildConfigFromHaloydDeployments converts the haloyd deployment format to proxy config.
func BuildConfigFromHaloydDeployments(deployments map[string]HaloydDeployment, apiDomain string) (*Config, error) {
	rb := NewRouteBuilder()
	rb.SetAPIDomain(apiDomain)

	for _, d := range deployments {
		for _, domain := range d.Domains {
			if domain.Canonical == "" {
				continue
			}

			backends := make([]Backend, 0, len(d.Instances))
			for _, inst := range d.Instances {
				backends = append(backends, Backend{
					IP:   inst.IP,
					Port: inst.Port,
				})
			}

			rb.AddRoute(domain.Canonical, domain.Aliases, backends)
		}
	}

	return rb.Build()
}
