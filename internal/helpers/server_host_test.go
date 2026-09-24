package helpers

import (
	"strings"
	"testing"
)

func TestValidateServerHost(t *testing.T) {
	tests := []struct {
		name     string
		hostPort string
		wantErr  string
	}{
		{name: "domain", hostPort: "haloy.example.com"},
		{name: "domain with port", hostPort: "haloy.example.com:8443"},
		{name: "ipv4", hostPort: "203.0.113.10"},
		{name: "ipv4 with port", hostPort: "127.0.0.1:9922"},
		{name: "localhost", hostPort: "localhost"},
		{name: "localhost with port", hostPort: "localhost:9922"},
		{name: "ipv6", hostPort: "[::1]"},
		{name: "ipv6 with port", hostPort: "[::1]:9922"},

		{name: "empty", hostPort: "", wantErr: "no host"},
		{name: "port out of range", hostPort: "127.0.0.1:99999", wantErr: "invalid port"},
		{name: "not a domain", hostPort: "no_underscores_here.com", wantErr: "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServerHost(tt.hostPort)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateServerHost(%q) error = %v, want nil", tt.hostPort, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateServerHost(%q) error = %v, want it to contain %q", tt.hostPort, err, tt.wantErr)
			}
		})
	}
}

// The address the CLI validates is the one NormalizeServerURL produces, so
// the two have to agree about what a server address looks like.
func TestValidateServerHostAcceptsWhatNormalizeProduces(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1:9922",
		"http://127.0.0.1:9922",
		"https://haloy.example.com",
		"https://haloy.example.com:8443",
		"203.0.113.10",
	} {
		normalized, err := NormalizeServerURL(raw)
		if err != nil {
			t.Fatalf("NormalizeServerURL(%q) error = %v", raw, err)
		}
		if err := ValidateServerHost(normalized); err != nil {
			t.Errorf("NormalizeServerURL(%q) = %q, which ValidateServerHost rejects: %v", raw, normalized, err)
		}
	}
}
