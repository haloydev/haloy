package haloyd

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/healthcheck"
	"github.com/haloydev/haloy/internal/proxyclient"
	"github.com/haloydev/haloy/internal/proxywire"
)

func summary(app, id, name, state, ip string) container.Summary {
	return container.Summary{ID: name, State: state, Labels: map[string]string{config.LabelAppName: app, config.LabelDeploymentID: id}, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{constants.DockerNetwork: {IPAddress: ip}}}}
}

func TestPlanRetirement(t *testing.T) {
	var cs []container.Summary
	for _, state := range []string{"running", "restarting", "paused", "created", "exited", "dead"} {
		cs = append(cs, summary("a", "001", state, state, ""))
	}
	cs = append(cs, summary("a", "003", "next", "running", ""), summary("a", "002", "current", "running", ""), summary("a", "", "unlabelled", "exited", ""), summary("b", "001", "other", "running", ""))
	plan := planRetirement("a", "002", cs)
	if !slices.Equal(plan.Stop, []string{"running", "restarting", "paused"}) || !slices.Equal(plan.Remove, []string{"running", "restarting", "paused", "created", "exited", "dead"}) {
		t.Fatalf("unexpected plan %+v", plan)
	}
}

type fakeRuntime struct {
	containers       []container.Summary
	onList           func()
	stopped, removed []string
}

func (r *fakeRuntime) List(context.Context, string) ([]container.Summary, error) {
	cs := slices.Clone(r.containers)
	if r.onList != nil {
		r.onList()
	}
	return cs, nil
}

func (r *fakeRuntime) Stop(_ context.Context, _ *slog.Logger, ids []string) error {
	r.stopped = append(r.stopped, ids...)
	return nil
}

func (r *fakeRuntime) Remove(_ context.Context, _ *slog.Logger, ids []string) error {
	r.removed = append(r.removed, ids...)
	return nil
}

type fakeCerts struct {
	err                        error
	syncs, refreshes, cleanups int
}

func (c *fakeCerts) RefreshSync(*slog.Logger, []CertificatesDomain) error          { c.syncs++; return c.err }
func (c *fakeCerts) Refresh(*slog.Logger, []CertificatesDomain)                    { c.refreshes++ }
func (c *fakeCerts) CleanupExpiredCertificates(*slog.Logger, []CertificatesDomain) { c.cleanups++ }

type pushFunc func(context.Context, *proxywire.Snapshot) error

func (f pushFunc) Push(ctx context.Context, s *proxywire.Snapshot) error { return f(ctx, s) }

func TestRetirementDuringEligibleBatch(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		certErr, pushErr             error
		state, ip                    string
		exclude, stopOnly, emptyPlan bool
		wantLeftInPlace              bool
		wantRetired                  bool
	}{
		{name: "another app promoted first", state: "running", ip: "10.0.0.2", wantRetired: true},
		{name: "certificate failure", certErr: errors.New("cert")},
		{name: "hard push failure", pushErr: errors.New("push")},
		{name: "stop-only outage", stopOnly: true, pushErr: proxyclient.ErrUnreachable},
		{name: "empty plan without a running backend", emptyPlan: true, state: "exited"},
		{name: "proxy outage", pushErr: proxyclient.ErrUnreachable},
		{name: "routed replica exited", wantLeftInPlace: true, state: "exited", ip: "10.0.0.2"},
		{name: "routed replica changed IP", wantLeftInPlace: true, state: "running", ip: "10.0.0.3"},
		{name: "excluded while listing", wantLeftInPlace: true, state: "running", ip: "10.0.0.2", exclude: true},
		{name: "stop only", state: "running", ip: "10.0.0.2", stopOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
			a := healthyContainerFor("a", "002", "new")
			a.IP = "10.0.0.2"
			healthy := []HealthyContainer{healthyContainerFor("a", "001", "old"), a, healthyContainerFor("b", "001", "other")}
			dm := NewDeploymentManager(nil, nil)
			certs := &fakeCerts{}
			var pushErr error
			u := NewUpdater(UpdaterConfig{DeploymentManager: dm, ProxyPusher: pushFunc(func(context.Context, *proxywire.Snapshot) error { return pushErr })})
			u.certManager = certs
			runtime := &fakeRuntime{containers: []container.Summary{summary("a", "001", "old", "running", ""), summary("a", "002", "new", tc.state, tc.ip), summary("a", "002", "unrouted", "running", "10.0.0.9")}}
			if tc.emptyPlan {
				runtime.containers = runtime.containers[1:]
			}
			u.runtime = runtime
			b := &TriggeredByApp{appName: "b", deploymentID: "001"}
			if err := u.reconcile(t.Context(), logger, TriggerReasonAppUpdated, b, healthy); err != nil {
				t.Fatal(err)
			}
			certs.err = tc.certErr
			pushErr = tc.pushErr
			if pushErr != nil {
				// A changed endpoint forces a new publication for the eligible batch.
				healthy[1].Port = "9090"
			}
			runtime.onList = func() {
				runtime.containers = append(runtime.containers, summary("a", "003", "next", "running", ""))
				if tc.exclude {
					h := HealthConfigUpdater{u.publisher, logger}
					h.OnHealthChange([]healthcheck.Target{{ID: "new"}})
				}
			}
			app := &TriggeredByApp{appName: "a", deploymentID: "002", startedSelected: !tc.stopOnly}
			refreshesBefore, syncsBefore := certs.refreshes, certs.syncs
			err := u.reconcile(t.Context(), logger, TriggerReasonAppUpdated, app, healthy)
			if hardPushFailure := tc.pushErr != nil && tc.pushErr != proxyclient.ErrUnreachable; hardPushFailure &&
				(certs.refreshes != refreshesBefore+1 || certs.syncs != syncsBefore) {
				t.Fatalf("hard push failure must still run async certificate maintenance only: %+v", certs)
			}
			wantErr := tc.certErr
			if tc.pushErr != nil && tc.pushErr != proxyclient.ErrUnreachable {
				wantErr = tc.pushErr
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error %v, want %v", err, wantErr)
			}
			output := logs.String()
			if strings.Contains(output, "no healthy backends") {
				t.Fatalf("publisher warning leaked into the deployment log: %s", output)
			}
			wantPossible := tc.pushErr == proxyclient.ErrUnreachable && !tc.stopOnly
			if strings.Contains(output, "Older containers may still be running") != wantPossible ||
				strings.Contains(output, "Left superseded containers in place") != tc.wantLeftInPlace {
				t.Fatalf("unexpected cleanup warnings: %s", output)
			}
			if (wantPossible || tc.wantLeftInPlace) && !strings.Contains(output, "app=a deployment_id=002") {
				t.Fatalf("missing deployment context: %s", output)
			}
			if tc.wantLeftInPlace && !strings.Contains(output, "container_count=1") {
				t.Fatalf("missing candidate count: %s", output)
			}
			var want []string
			if tc.wantRetired {
				want = []string{"old"}
				if certs.syncs != 1 {
					t.Fatal("missing certificate check")
				}
			}
			if !slices.Equal(runtime.stopped, want) || !slices.Equal(runtime.removed, want) {
				t.Fatalf("stopped %v, removed %v", runtime.stopped, runtime.removed)
			}
		})
	}
}

func TestPeriodicPublicationRecovery(t *testing.T) {
	certs := &fakeCerts{}
	calls := 0
	u := NewUpdater(UpdaterConfig{DeploymentManager: NewDeploymentManager(nil, nil), ProxyPusher: pushFunc(func(context.Context, *proxywire.Snapshot) error {
		calls++
		if calls == 1 {
			return errors.New("disk full")
		}
		return nil
	})})
	u.certManager = certs
	for i := 0; i < 2; i++ {
		err := u.reconcile(t.Context(), discardLogger(), TriggerPeriodicRefresh, nil, nil)
		if (err != nil) != (i == 0) || certs.refreshes != i+1 || certs.cleanups != i {
			t.Fatalf("round %d: error %v certs %+v", i, err, certs)
		}
	}
	if calls != 2 || !u.publisher.confirmed() {
		t.Fatal("unchanged retry did not publish")
	}
}
