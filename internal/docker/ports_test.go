package docker

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/haloydev/haloy/internal/config"
)

func TestPortBindings(t *testing.T) {
	t.Run("no ports leaves docker defaults untouched", func(t *testing.T) {
		exposed, bindings, err := portBindings(config.TargetConfig{Port: "8080"})
		if err != nil {
			t.Fatalf("portBindings() unexpected error = %v", err)
		}
		if exposed != nil || bindings != nil {
			t.Fatalf("portBindings() = %v, %v, want nil, nil", exposed, bindings)
		}
	})

	t.Run("binds every published port and keeps the http port exposed", func(t *testing.T) {
		exposed, bindings, err := portBindings(config.TargetConfig{
			Port:  "8080",
			Ports: []string{"8890:8890", "9000:8809/udp", "127.0.0.1:8891:8891", "49152-49154:49152-49154/udp"},
		})
		if err != nil {
			t.Fatalf("portBindings() unexpected error = %v", err)
		}

		wantBindings := nat.PortMap{
			"8890/tcp":  {{HostPort: "8890"}},
			"8809/udp":  {{HostPort: "9000"}},
			"8891/tcp":  {{HostIP: "127.0.0.1", HostPort: "8891"}},
			"49152/udp": {{HostPort: "49152"}},
			"49153/udp": {{HostPort: "49153"}},
			"49154/udp": {{HostPort: "49154"}},
		}
		if len(bindings) != len(wantBindings) {
			t.Fatalf("bindings = %v, want %v", bindings, wantBindings)
		}
		for port, want := range wantBindings {
			got := bindings[port]
			if len(got) != 1 || got[0] != want[0] {
				t.Errorf("bindings[%s] = %v, want %v", port, got, want)
			}
		}

		for port := range wantBindings {
			if _, ok := exposed[port]; !ok {
				t.Errorf("exposed ports %v missing %s", exposed, port)
			}
		}
		if _, ok := exposed["8080/tcp"]; !ok {
			t.Errorf("exposed ports %v missing the http port 8080/tcp", exposed)
		}
	})

	t.Run("one container port on two host ips gets two bindings", func(t *testing.T) {
		_, bindings, err := portBindings(config.TargetConfig{
			Port:  "8080",
			Ports: []string{"127.0.0.1:8891:8891", "10.0.0.2:8891:8891"},
		})
		if err != nil {
			t.Fatalf("portBindings() unexpected error = %v", err)
		}
		if got := bindings["8891/tcp"]; len(got) != 2 {
			t.Fatalf("bindings[8891/tcp] = %v, want two bindings", got)
		}
	})
}

func TestFindPortConflict(t *testing.T) {
	published, err := config.ParsePortSpecs([]string{"8890:8890", "49152-49351:49152-49351/udp", "127.0.0.1:8891:8891"})
	if err != nil {
		t.Fatalf("ParsePortSpecs() unexpected error = %v", err)
	}

	appContainer := func(app string, ports ...container.Port) container.Summary {
		return container.Summary{
			ID:     "0123456789abcdef",
			Names:  []string{"/" + app},
			Labels: map[string]string{config.LabelAppName: app},
			Ports:  ports,
		}
	}

	tests := []struct {
		name    string
		running []container.Summary
		wantErr string
	}{
		{
			name: "own app's container is ignored",
			running: []container.Summary{
				appContainer("noray", container.Port{IP: "0.0.0.0", PrivatePort: 8890, PublicPort: 8890, Type: "tcp"}),
			},
		},
		{
			name: "other app on a different protocol",
			running: []container.Summary{
				appContainer("dns", container.Port{IP: "0.0.0.0", PrivatePort: 8890, PublicPort: 8890, Type: "udp"}),
			},
		},
		{
			name: "exposed but unpublished port",
			running: []container.Summary{
				appContainer("web", container.Port{PrivatePort: 8890, Type: "tcp"}),
			},
		},
		{
			name: "other app on a different specific ip",
			running: []container.Summary{
				appContainer("metrics", container.Port{IP: "10.0.0.2", PrivatePort: 8891, PublicPort: 8891, Type: "tcp"}),
			},
		},
		{
			name: "other app inside the udp range",
			running: []container.Summary{
				appContainer("turn", container.Port{IP: "::", PrivatePort: 3478, PublicPort: 49200, Type: "udp"}),
			},
			wantErr: "host port 49200/udp from '49152-49351:49152-49351/udp' is already published by container of app 'turn'",
		},
		{
			name: "unmanaged container on all interfaces",
			running: []container.Summary{
				{ID: "fedcba9876543210", Names: []string{"/legacy"}, Ports: []container.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8891, Type: "tcp"}}},
			},
			wantErr: "already published by container /legacy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := findPortConflict(published, tt.running, "noray")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("findPortConflict() unexpected error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("findPortConflict() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
