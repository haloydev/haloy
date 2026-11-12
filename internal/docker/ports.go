package docker

import (
	"context"
	"fmt"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/haloydev/haloy/internal/config"
)

// portBindings builds the exposed ports and host bindings for a target's
// published ports. When ports are published, the HTTP port is also exposed so
// haloyd's port check still finds it on images that declare no EXPOSE.
func portBindings(targetConfig config.TargetConfig) (nat.PortSet, nat.PortMap, error) {
	if len(targetConfig.Ports) == 0 {
		return nil, nil, nil
	}

	published, err := config.ParsePortSpecs(targetConfig.Ports)
	if err != nil {
		return nil, nil, err
	}

	exposed := make(nat.PortSet, len(published)+1)
	bindings := make(nat.PortMap, len(published))

	if targetConfig.Port != "" {
		httpPort, err := nat.NewPort("tcp", targetConfig.Port.String())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid port %s: %w", targetConfig.Port, err)
		}
		exposed[httpPort] = struct{}{}
	}

	for _, p := range published {
		containerPort, err := nat.NewPort(p.Protocol, strconv.Itoa(p.ContainerPort))
		if err != nil {
			return nil, nil, err
		}
		exposed[containerPort] = struct{}{}
		bindings[containerPort] = append(bindings[containerPort], nat.PortBinding{
			HostIP:   p.HostIP,
			HostPort: strconv.Itoa(p.HostPort),
		})
	}

	return exposed, bindings, nil
}

// CheckPublishedPortConflicts fails when a running container of another app
// already publishes one of the target's host ports. It runs before a replace
// deploy stops the current containers, so a conflict leaves them running.
func CheckPublishedPortConflicts(ctx context.Context, cli *client.Client, targetConfig config.TargetConfig) error {
	if len(targetConfig.Ports) == 0 {
		return nil
	}

	published, err := config.ParsePortSpecs(targetConfig.Ports)
	if err != nil {
		return err
	}

	running, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	return findPortConflict(published, running, targetConfig.Name)
}

// findPortConflict returns an error naming the first container, other than
// the app's own, that publishes a host port overlapping one in published.
func findPortConflict(published []config.PublishedPort, running []container.Summary, appName string) error {
	for _, c := range running {
		if c.Labels[config.LabelAppName] == appName {
			continue
		}
		for _, used := range c.Ports {
			if used.PublicPort == 0 {
				continue
			}
			for _, p := range published {
				if p.Protocol == used.Type && p.HostPort == int(used.PublicPort) && config.HostIPsOverlap(p.HostIP, used.IP) {
					return fmt.Errorf("host port %s from '%s' is already published by container %s", p.HostAddr(), p.Raw, containerDisplayName(c))
				}
			}
		}
	}
	return nil
}

func containerDisplayName(c container.Summary) string {
	if app := c.Labels[config.LabelAppName]; app != "" {
		return fmt.Sprintf("of app '%s'", app)
	}
	if len(c.Names) > 0 {
		return c.Names[0]
	}
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}
