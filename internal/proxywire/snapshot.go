// Package proxywire defines the wire format exchanged between haloyd (the
// control plane) and haloy-proxy (the data plane): a JSON routing snapshot
// pushed over the proxy control socket and persisted as an atomic snapshot
// file so the proxy can boot with last-known-good routes while haloyd is down.
//
// Versioning policy: additive optional fields do NOT bump SchemaVersion
// (unknown JSON fields are ignored by older proxies). Semantics-breaking
// changes bump SchemaVersion; the proxy rejects snapshots with a newer schema
// than it supports. When a schema bump ships, upgrade haloy-proxy before
// haloyd.
package proxywire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// SchemaVersion is the highest snapshot schema version this build understands.
const SchemaVersion = 1

// ErrSchemaTooNew indicates a snapshot with a schema version newer than this
// build supports. The receiver should keep its current config and ask the
// operator to upgrade haloy-proxy.
var ErrSchemaTooNew = errors.New("snapshot schema version is newer than supported")

// Snapshot is a complete routing configuration for the proxy.
type Snapshot struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at,omitzero"`
	APIDomain     string    `json:"api_domain,omitempty"`
	// APIBackend is haloyd's loopback API listener; the proxy forwards
	// API-domain and localhost API traffic to it.
	APIBackend *Backend `json:"api_backend,omitempty"`
	Routes     []Route  `json:"routes"`
}

// Route maps a canonical domain (plus aliases) to its backends. A route with
// no backends is valid: the proxy serves 502 instead of 404 for it.
type Route struct {
	Canonical string    `json:"canonical"`
	Aliases   []string  `json:"aliases,omitempty"`
	Backends  []Backend `json:"backends,omitempty"`
}

// Backend is a single upstream address.
type Backend struct {
	IP   string `json:"ip"`
	Port string `json:"port"`
}

// CheckSchemaVersion returns ErrSchemaTooNew if the snapshot was produced by a
// newer haloyd than this build understands.
func (s *Snapshot) CheckSchemaVersion() error {
	if s.SchemaVersion > SchemaVersion {
		return fmt.Errorf("%w: snapshot has version %d, max supported is %d",
			ErrSchemaTooNew, s.SchemaVersion, SchemaVersion)
	}
	return nil
}

// Hash returns a stable sha256 hex digest of the snapshot's routing content.
// Routes, aliases and backends are sorted before hashing and GeneratedAt is
// excluded, so two snapshots describing the same routing hash identically.
func (s *Snapshot) Hash() string {
	routes := make([]Route, len(s.Routes))
	for i, r := range s.Routes {
		routes[i] = Route{
			Canonical: r.Canonical,
			Aliases:   slices.Sorted(slices.Values(r.Aliases)),
			Backends:  slices.Clone(r.Backends),
		}
		slices.SortFunc(routes[i].Backends, func(a, b Backend) int {
			return strings.Compare(a.IP+":"+a.Port, b.IP+":"+b.Port)
		})
	}
	slices.SortFunc(routes, func(a, b Route) int {
		return strings.Compare(a.Canonical, b.Canonical)
	})

	content := Snapshot{
		SchemaVersion: s.SchemaVersion,
		APIDomain:     s.APIDomain,
		APIBackend:    s.APIBackend,
		Routes:        routes,
	}
	data, err := json.Marshal(content)
	if err != nil {
		// Snapshot contains only plain marshalable types; this cannot happen.
		panic(fmt.Sprintf("proxywire: marshal snapshot for hashing: %v", err))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Status is the payload of the proxy control API's status endpoint.
type Status struct {
	// Version is the haloy-proxy build version.
	Version string `json:"version"`
	// SchemaVersion is the highest snapshot schema the proxy supports.
	SchemaVersion int `json:"schema_version"`
	// ConfigHash is the Hash() of the currently applied snapshot.
	ConfigHash string `json:"config_hash,omitempty"`
	// Routes is the number of canonical domains currently routed.
	Routes int `json:"routes"`
	// LoadedFrom reports where the current config came from:
	// "socket", "snapshot-file" or "empty".
	LoadedFrom string `json:"loaded_from"`
	// LastUpdateAt is when the config was last applied.
	LastUpdateAt time.Time `json:"last_update_at,omitzero"`
	// CertsLoaded is the number of TLS certificates in the proxy's cache.
	CertsLoaded int `json:"certs_loaded"`
}
