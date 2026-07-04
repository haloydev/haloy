package proxy

import (
	"fmt"
	"strings"
)

// RouteBuilder helps build proxy routes from deployment information.
type RouteBuilder struct {
	routes     map[string]*Route
	apiDomain  string
	apiBackend Backend
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

// SetAPIBackend sets the control plane's API listener address, which the
// proxy forwards API-domain and localhost API traffic to.
func (rb *RouteBuilder) SetAPIBackend(ip, port string) {
	rb.apiBackend = Backend{IP: ip, Port: port}
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

// Build validates the routes and creates the final proxy configuration with a
// flat host lookup index. It returns an error if a domain is used as both a
// canonical domain and an alias, or as an alias of multiple routes.
func (rb *RouteBuilder) Build() (*Config, error) {
	hosts := make(map[string]*Route, len(rb.routes))
	owner := make(map[string]string, len(rb.routes)) // host -> canonical that owns it

	for canonical, route := range rb.routes {
		owner[canonical] = canonical
		hosts[canonical] = route
	}

	for canonical, route := range rb.routes {
		for _, alias := range route.Aliases {
			if prev, exists := owner[alias]; exists {
				if prev == alias {
					return nil, fmt.Errorf("domain %q is both a canonical domain and an alias of %q", alias, canonical)
				}
				return nil, fmt.Errorf("alias %q is used by both %q and %q", alias, prev, canonical)
			}
			owner[alias] = canonical
			hosts[alias] = route
		}
	}

	return &Config{
		routes:     rb.routes,
		hosts:      hosts,
		apiDomain:  rb.apiDomain,
		apiBackend: rb.apiBackend,
	}, nil
}
