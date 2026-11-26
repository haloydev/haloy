package haloyd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/docker"
)

type retirement struct{ Stop, Remove []string }

func planRetirement(app, current string, containers []container.Summary) retirement {
	var plan retirement
	if app == "" || current == "" {
		return plan
	}
	for _, c := range containers {
		id := c.Labels[config.LabelDeploymentID]
		if c.Labels[config.LabelAppName] != app || id == "" || id >= current {
			continue
		}
		plan.Remove = append(plan.Remove, c.ID)
		switch c.State {
		case "running", "paused", "restarting":
			plan.Stop = append(plan.Stop, c.ID)
		}
	}
	return plan
}

type containerRuntime interface {
	List(context.Context, string) ([]container.Summary, error)
	Stop(context.Context, *slog.Logger, []string) error
	Remove(context.Context, *slog.Logger, []string) error
}
type dockerRuntime struct{ cli *client.Client }

func (r dockerRuntime) List(ctx context.Context, app string) ([]container.Summary, error) {
	return docker.GetAppContainers(ctx, r.cli, true, app)
}

func (r dockerRuntime) Stop(ctx context.Context, l *slog.Logger, ids []string) error {
	return docker.StopContainersByID(ctx, r.cli, l, ids)
}

func (r dockerRuntime) Remove(ctx context.Context, l *slog.Logger, ids []string) error {
	return docker.RemoveContainersByID(ctx, r.cli, l, ids)
}

func (u *Updater) retire(ctx context.Context, logger *slog.Logger, app *TriggeredByApp) error {
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	containers, err := u.runtime.List(stopCtx, app.appName)
	if err != nil {
		return fmt.Errorf("failed to list containers for retirement: %w", err)
	}
	plan := planRetirement(app.appName, app.deploymentID, containers)
	if len(plan.Remove) == 0 {
		return nil
	}
	if !u.publisher.serving(app.appName, app.deploymentID, containers) {
		logger.Warn("Left superseded containers in place because no confirmed route matched a running backend for this deployment",
			"app", app.appName, "deployment_id", app.deploymentID, "container_count", len(plan.Remove))
		return nil
	}
	if len(plan.Stop) > 0 {
		if err := u.runtime.Stop(stopCtx, logger, plan.Stop); err != nil {
			return fmt.Errorf("failed to stop old containers: %w", err)
		}
	}
	if err := u.runtime.Remove(stopCtx, logger, plan.Remove); err != nil {
		return fmt.Errorf("failed to remove old containers: %w", err)
	}
	return nil
}
