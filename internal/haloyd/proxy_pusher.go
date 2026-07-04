package haloyd

import (
	"context"

	"github.com/haloydev/haloy/internal/proxywire"
)

// ProxyPusher delivers routing snapshots to the proxy. In production this is
// a *proxyclient.Client pushing to the haloy-proxy control socket; tests use
// an in-process implementation.
type ProxyPusher interface {
	Push(ctx context.Context, snap *proxywire.Snapshot) error
}
