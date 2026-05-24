package haloy

import (
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func TestResolveTunnelPorts(t *testing.T) {
	target := config.TargetConfig{
		Name: "postgres",
		Port: config.Port("5432"),
	}

	tests := []struct {
		name           string
		args           []string
		localPortFlag  string
		remotePortFlag string
		wantLocal      string
		wantRemote     string
	}{
		{
			name:      "defaults local port from target config",
			wantLocal: "5432",
		},
		{
			name:          "uses port flag as local port",
			localPortFlag: "25432",
			wantLocal:     "25432",
		},
		{
			name:      "uses positional argument as local port",
			args:      []string{"15432"},
			wantLocal: "15432",
		},
		{
			name:           "uses remote port flag as remote override",
			remotePortFlag: "15432",
			wantLocal:      "5432",
			wantRemote:     "15432",
		},
		{
			name:           "uses local and remote overrides together",
			localPortFlag:  "25432",
			remotePortFlag: "5433",
			wantLocal:      "25432",
			wantRemote:     "5433",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocal, gotRemote, err := resolveTunnelPorts(target, tt.args, tt.localPortFlag, tt.remotePortFlag)
			if err != nil {
				t.Fatalf("resolveTunnelPorts error = %v", err)
			}
			if gotLocal != tt.wantLocal {
				t.Fatalf("local port = %q, want %q", gotLocal, tt.wantLocal)
			}
			if gotRemote != tt.wantRemote {
				t.Fatalf("remote port = %q, want %q", gotRemote, tt.wantRemote)
			}
		})
	}
}

func TestResolveTunnelPortsErrors(t *testing.T) {
	target := config.TargetConfig{
		Name: "postgres",
		Port: config.Port("5432"),
	}

	tests := []struct {
		name           string
		target         config.TargetConfig
		args           []string
		localPortFlag  string
		remotePortFlag string
		wantErr        string
	}{
		{
			name:          "rejects duplicate local port",
			target:        target,
			args:          []string{"15432"},
			localPortFlag: "25432",
			wantErr:       "local port specified twice",
		},
		{
			name:    "requires local port when target has no configured port",
			target:  config.TargetConfig{Name: "worker"},
			wantErr: "no port configured for target",
		},
		{
			name:          "rejects invalid local port",
			target:        target,
			localPortFlag: "70000",
			wantErr:       "invalid local port",
		},
		{
			name:           "rejects invalid remote port",
			target:         target,
			remotePortFlag: "not-a-port",
			wantErr:        "invalid remote port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := resolveTunnelPorts(tt.target, tt.args, tt.localPortFlag, tt.remotePortFlag)
			if err == nil {
				t.Fatal("resolveTunnelPorts error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("resolveTunnelPorts error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}
