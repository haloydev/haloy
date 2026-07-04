package proxy

import (
	"fmt"

	"github.com/haloydev/haloy/internal/proxywire"
)

// ConfigFromSnapshot validates and converts a wire snapshot into an immutable
// routing Config, applying the same duplicate-domain validation as
// RouteBuilder. The round-robin state of each route starts fresh.
func ConfigFromSnapshot(snap *proxywire.Snapshot) (*Config, error) {
	if snap == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}
	if err := snap.CheckSchemaVersion(); err != nil {
		return nil, err
	}

	rb := NewRouteBuilder()
	rb.SetAPIDomain(snap.APIDomain)
	if snap.APIBackend != nil {
		rb.SetAPIBackend(snap.APIBackend.IP, snap.APIBackend.Port)
	}

	for _, route := range snap.Routes {
		if route.Canonical == "" {
			continue
		}
		var backends []Backend
		for _, b := range route.Backends {
			backends = append(backends, Backend{IP: b.IP, Port: b.Port})
		}
		rb.AddRoute(route.Canonical, route.Aliases, backends)
	}

	return rb.Build()
}
