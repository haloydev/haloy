package haloyd

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

type routePublisher struct {
	mu        sync.Mutex
	dm        *DeploymentManager
	pusher    ProxyPusher
	apiDomain string
	unhealthy map[string]bool
	// nil until the latest publication has been acknowledged.
	accepted map[string]Deployment
	// starved holds the apps that had no healthy backends at the last
	// acknowledged publication, so the warning is logged once per transition
	// instead of on every publication.
	starved map[string]bool
	// logger is the daemon logger. Publications are triggered by deployments
	// of unrelated apps and by the health monitor, so messages about an app's
	// backends must not go to the caller's log stream.
	logger *slog.Logger
}

func (p *routePublisher) log() *slog.Logger {
	if p.logger != nil {
		return p.logger
	}
	return slog.Default()
}

func (p *routePublisher) confirmed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepted != nil
}

func (p *routePublisher) publish(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.publishLocked(ctx)
}

func (p *routePublisher) publishLocked(ctx context.Context) error {
	active, failed := p.dm.Snapshot()
	included := func(i DeploymentInstance) bool { return !p.unhealthy[i.ContainerID] }
	snap := buildSnapshot(active, failed, p.apiDomain, included)
	if err := p.pusher.Push(ctx, snap); err != nil {
		p.accepted = nil
		return err
	}
	claims := make(map[string]int)
	for _, route := range snap.Routes {
		claims[strings.ToLower(route.Canonical)]++
	}
	accepted := make(map[string]Deployment)
	starved := make(map[string]bool)
	for app, d := range active {
		var instances []DeploymentInstance
		for _, i := range d.Instances {
			if included(i) {
				instances = append(instances, i)
			}
		}
		if len(instances) == 0 {
			starved[app] = true
			if !p.starved[app] {
				p.log().Warn("App has no healthy backends", "app", app, "total_instances", len(d.Instances))
			}
			continue
		}
		if p.starved[app] {
			p.log().Info("App has healthy backends again", "app", app, "healthy_instances", len(instances))
		}
		for _, domain := range d.Labels.Domains {
			host := strings.ToLower(domain.Canonical)
			if claims[host] == 1 && !strings.EqualFold(host, p.apiDomain) {
				d.Instances = instances
				accepted[app] = d
				break
			}
		}
	}
	p.accepted = accepted
	p.starved = starved
	return nil
}

func (p *routePublisher) serving(app, id string, containers []container.Summary) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.accepted[app]
	if !ok || d.Labels.DeploymentID != id {
		return false
	}
	for _, c := range containers {
		if c.State != "running" || c.Labels[config.LabelAppName] != app || c.Labels[config.LabelDeploymentID] != id || c.NetworkSettings == nil {
			continue
		}
		net := c.NetworkSettings.Networks[constants.DockerNetwork]
		if net == nil || net.IPAddress == "" {
			continue
		}
		for _, i := range d.Instances {
			if i.ContainerID == c.ID && i.IP == net.IPAddress {
				return true
			}
		}
	}
	return false
}
