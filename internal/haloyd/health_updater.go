package haloyd

import (
	"context"
	"log/slog"
	"time"

	"github.com/haloydev/haloy/internal/healthcheck"
)

type HealthConfigUpdater struct {
	publisher *routePublisher
	logger    *slog.Logger
}

func (u *HealthConfigUpdater) OnHealthChange(unhealthy []healthcheck.Target) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := u.publisher
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unhealthy = make(map[string]bool, len(unhealthy))
	for _, target := range unhealthy {
		p.unhealthy[target.ID] = true
	}
	if err := p.publishLocked(ctx); err != nil {
		u.logger.Error("Failed to publish health update", "error", err)
	}
}
