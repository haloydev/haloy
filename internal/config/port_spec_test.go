package config

import (
	"strings"
	"testing"
)

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []PublishedPort
		wantLen int
		wantErr string
	}{
		{
			name: "tcp is the default protocol",
			raw:  "8890:8890",
			want: []PublishedPort{{HostPort: 8890, ContainerPort: 8890, Protocol: "tcp"}},
		},
		{
			name: "udp with different host and container ports",
			raw:  "9000:8809/udp",
			want: []PublishedPort{{HostPort: 9000, ContainerPort: 8809, Protocol: "udp"}},
		},
		{
			name: "protocol is case insensitive",
			raw:  "8809:8809/UDP",
			want: []PublishedPort{{HostPort: 8809, ContainerPort: 8809, Protocol: "udp"}},
		},
		{
			name: "host ip binding",
			raw:  "127.0.0.1:8891:8891",
			want: []PublishedPort{{HostIP: "127.0.0.1", HostPort: 8891, ContainerPort: 8891, Protocol: "tcp"}},
		},
		{
			name: "ipv6 host ip binding",
			raw:  "[::1]:8891:8891/tcp",
			want: []PublishedPort{{HostIP: "::1", HostPort: 8891, ContainerPort: 8891, Protocol: "tcp"}},
		},
		{
			name: "container port 8080 behind another host port is allowed",
			raw:  "3000:8080",
			want: []PublishedPort{{HostPort: 3000, ContainerPort: 8080, Protocol: "tcp"}},
		},
		{
			name: "udp on a reserved tcp port is allowed",
			raw:  "443:443/udp",
			want: []PublishedPort{{HostPort: 443, ContainerPort: 443, Protocol: "udp"}},
		},
		{
			name:    "range expands to one mapping per port",
			raw:     "49152-49351:49152-49351/udp",
			wantLen: 200,
		},
		{
			name: "shifted range keeps the offset",
			raw:  "10000-10001:20000-20001",
			want: []PublishedPort{
				{HostPort: 10000, ContainerPort: 20000, Protocol: "tcp"},
				{HostPort: 10001, ContainerPort: 20001, Protocol: "tcp"},
			},
		},
		{name: "missing host port", raw: "8890", wantErr: "has no host port"},
		{name: "missing host port with protocol", raw: "8809/udp", wantErr: "has no host port"},
		{name: "empty", raw: " ", wantErr: "expected '[host_ip:]host_port:container_port"},
		{name: "unequal ranges", raw: "8000-8002:8000-8001", wantErr: "invalid ranges"},
		{name: "host range to single container port", raw: "8000-8002:8000", wantErr: "same length"},
		{name: "sctp rejected", raw: "9000:9000/sctp", wantErr: "protocol must be 'tcp' or 'udp'"},
		{name: "unknown protocol", raw: "9000:9000/quic", wantErr: "invalid port mapping"},
		{name: "invalid host ip", raw: "300.1.1.1:9000:9000", wantErr: "invalid IP address"},
		{name: "port out of range", raw: "70000:9000", wantErr: "invalid port mapping"},
		{name: "port zero", raw: "0:9000", wantErr: "host port must be between 1 and 65535"},
		{name: "reserved proxy port", raw: "80:8080", wantErr: "haloy-proxy"},
		{name: "reserved https port with host ip", raw: "127.0.0.1:443:443", wantErr: "haloy-proxy"},
		{name: "reserved acme port", raw: "8080:8080", wantErr: "ACME"},
		{name: "reserved api port", raw: "9922:9922", wantErr: "API"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePortSpec(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParsePortSpec(%q) expected error containing %q, got %+v", tt.raw, tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParsePortSpec(%q) error = %q, want it to contain %q", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePortSpec(%q) unexpected error: %v", tt.raw, err)
			}
			if tt.wantLen != 0 {
				if len(got) != tt.wantLen {
					t.Fatalf("ParsePortSpec(%q) returned %d mappings, want %d", tt.raw, len(got), tt.wantLen)
				}
				first, last := got[0], got[len(got)-1]
				if first.HostPort != 49152 || last.HostPort != 49351 || last.ContainerPort != 49351 || last.Protocol != "udp" {
					t.Fatalf("ParsePortSpec(%q) range bounds = %+v .. %+v", tt.raw, first, last)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParsePortSpec(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
			for i := range got {
				want := tt.want[i]
				want.Raw = tt.raw
				if got[i] != want {
					t.Errorf("ParsePortSpec(%q)[%d] = %+v, want %+v", tt.raw, i, got[i], want)
				}
			}
		})
	}
}

func TestParsePortSpecs_RejectsOverlaps(t *testing.T) {
	tests := []struct {
		name    string
		specs   []string
		wantErr bool
	}{
		{name: "same port on tcp and udp", specs: []string{"8890:8890", "8890:8890/udp"}},
		{name: "same port on two specific ips", specs: []string{"127.0.0.1:8891:8891", "10.0.0.5:8891:8891"}},
		{name: "duplicate spec", specs: []string{"8890:8890", "8890:8890"}, wantErr: true},
		{name: "same host port to two container ports", specs: []string{"9000:9000/udp", "9000:9001/udp"}, wantErr: true},
		{name: "all interfaces overlaps a specific ip", specs: []string{"8891:8891", "127.0.0.1:8891:8891"}, wantErr: true},
		{name: "unspecified ipv4 overlaps all", specs: []string{"0.0.0.0:8891:8891", "[::1]:8891:8891"}, wantErr: true},
		{name: "single port inside a range", specs: []string{"49152-49351:49152-49351/udp", "49200:49200/udp"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePortSpecs(tt.specs)
			if tt.wantErr && err == nil {
				t.Fatalf("ParsePortSpecs(%v) expected an overlap error", tt.specs)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParsePortSpecs(%v) unexpected error: %v", tt.specs, err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "both bind host port") {
				t.Fatalf("ParsePortSpecs(%v) error = %q, want an overlap error", tt.specs, err)
			}
		})
	}
}
