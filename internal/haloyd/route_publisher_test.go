package haloyd

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/healthcheck"
	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxywire"
)

func TestContestedCanonicalCannotAuthorizeRetirement(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)
	a := healthyContainerFor("a", "002", "new")
	b := healthyContainerFor("b", "001", "other")
	b.Labels.Domains = []config.Domain{{Canonical: "A.EXAMPLE.COM"}, {Canonical: "api.example.com"}}
	c := healthyContainerFor("c", "001", "third")
	dm.UpdateDeployments([]HealthyContainer{a, b, c})
	server := proxy.New(discardLogger(), noopCertLoader{})
	p := routePublisher{dm: dm, pusher: newInProcessPusher(server), apiDomain: "api.example.com"}
	publish := func() {
		t.Helper()
		if err := p.publish(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	if _, ok := p.accepted["a"]; ok {
		t.Fatal("contested canonical accepted for a")
	}
	if _, ok := p.accepted["b"]; ok {
		t.Fatal("contested or API canonical accepted for b")
	}
	if server.GetConfig().FindRoute("c.example.com") == nil || len(p.accepted["c"].Instances) != 1 {
		t.Fatal("unrelated route lost")
	}
	a.Labels.Domains = append(a.Labels.Domains, config.Domain{Canonical: "another.example.com"})
	publish()
	if len(p.accepted["a"].Instances) != 1 {
		t.Fatal("uncontested canonical missing")
	}
}

func TestPublicationsSerializeAcrossPromotion(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)
	old := healthyContainerFor("a", "001", "old")
	current := healthyContainerFor("a", "002", "new")
	current.IP = "10.0.0.2"
	dm.UpdateDeployments([]HealthyContainer{old})
	entered, release := make(chan struct{}), make(chan struct{})
	var last *proxywire.Snapshot
	calls := 0
	p := routePublisher{dm: dm, pusher: pushFunc(func(_ context.Context, s *proxywire.Snapshot) error {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
		last = s
		return nil
	})}
	h := HealthConfigUpdater{&p, discardLogger()}
	go h.OnHealthChange([]healthcheck.Target{{ID: "old"}})
	<-entered
	dm.UpdateDeployments([]HealthyContainer{current})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := p.publish(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	if p.mu.TryLock() {
		p.mu.Unlock()
		t.Error("publisher unlocked during snapshot push")
	}
	close(release)
	<-done
	if backends := last.Routes[0].Backends; len(backends) != 1 || backends[0].IP != current.IP {
		t.Fatalf("stale publication %+v", last)
	}
	h.OnHealthChange([]healthcheck.Target{{ID: "old"}})
	if !p.serving("a", "002", []container.Summary{summary("a", "002", "new", "running", current.IP)}) {
		t.Fatal("stale round excluded new backend")
	}
	dm.UpdateDeployments([]HealthyContainer{old})
	if err := p.publish(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(last.Routes[0].Backends) != 0 {
		t.Fatal("updater lost monitor exclusion")
	}
}

func TestEndpointChangeTriggersPublication(t *testing.T) {
	dm := NewDeploymentManager(nil, nil)
	c := healthyContainerFor("a", "002", "new")
	dm.UpdateDeployments([]HealthyContainer{c})
	c.IP = "10.0.0.2"
	if !dm.UpdateDeployments([]HealthyContainer{c}) {
		t.Fatal("missed changed IP")
	}
	c.Port = "9090"
	if !dm.UpdateDeployments([]HealthyContainer{c}) {
		t.Fatal("missed changed port")
	}
}

func TestStarvedAppWarnsOncePerTransition(t *testing.T) {
	var logs bytes.Buffer
	dm := NewDeploymentManager(nil, nil)
	dm.UpdateDeployments([]HealthyContainer{healthyContainerFor("a", "001", "only")})
	p := routePublisher{
		dm:     dm,
		pusher: pushFunc(func(context.Context, *proxywire.Snapshot) error { return nil }),
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	publish := func(excluded bool) {
		t.Helper()
		p.unhealthy = map[string]bool{"only": excluded}
		if err := p.publish(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	count := func(msg string) int { return strings.Count(logs.String(), msg) }

	publish(true)
	publish(true)
	if count("App has no healthy backends") != 1 || !strings.Contains(logs.String(), "app=a total_instances=1") {
		t.Fatalf("want one warning with context, got: %s", logs.String())
	}
	publish(false)
	publish(false)
	if count("App has healthy backends again") != 1 {
		t.Fatalf("want one recovery line, got: %s", logs.String())
	}
	publish(true)
	if count("App has no healthy backends") != 2 {
		t.Fatalf("want a second warning after recovery, got: %s", logs.String())
	}
}
