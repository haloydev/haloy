package docker

import (
	"encoding/binary"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/haloydev/haloy/internal/config"
)

// testdata/proc_net holds tables captured on a little-endian (amd64/arm64)
// server; there is no udp6 table, as on a server with IPv6 disabled.
func TestReadHostSockets(t *testing.T) {
	sockets, err := readHostSockets("testdata/proc_net", binary.LittleEndian)
	if err != nil {
		t.Fatalf("readHostSockets() unexpected error = %v", err)
	}

	got := make([]string, 0, len(sockets))
	for _, s := range sockets {
		got = append(got, s.String())
	}
	want := []string{
		// tcp: listening sockets only, not the established ssh session
		"0.0.0.0:22/tcp",
		"127.0.0.1:9922/tcp",
		"127.0.0.1:5432/tcp",
		"0.0.0.0:8890/tcp",
		// tcp6: not the TIME_WAIT socket; an IPv4-mapped address reads as IPv4
		"[::]:22/tcp",
		"[::1]:5432/tcp",
		"127.0.0.1:6379/tcp",
		// udp: bound sockets, not the connected client on an ephemeral port
		"0.0.0.0:68/udp",
		"127.0.0.53:53/udp",
		"0.0.0.0:3478/udp",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("readHostSockets() = %v, want %v", got, want)
	}
}

func TestParseProcNetKeepsGoodLinesOnMalformedInput(t *testing.T) {
	table := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 19876 1
   1: garbage
   2: 0100007F:ZZZZ 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 19877 1
   3: 0100007F:26C2 00000000:0000 0A 00000000:00000000 00:00000000 00000000   999        0 21345 1
`
	sockets, err := parseProcNet(strings.NewReader(table), "tcp", binary.LittleEndian)
	if err == nil || !strings.Contains(err.Error(), "line 3") || !strings.Contains(err.Error(), "1 more malformed line") {
		t.Fatalf("parseProcNet() error = %v, want the first bad line and a count of the others", err)
	}
	if len(sockets) != 2 || sockets[0].Port != 22 || sockets[1].Port != 9922 {
		t.Fatalf("parseProcNet() = %v, want the two well-formed sockets", sockets)
	}
}

func TestFindHostSocketConflict(t *testing.T) {
	published, err := config.ParsePortSpecs([]string{"8890:8890", "49152-49351:49152-49351/udp", "10.0.0.2:5432:5432"})
	if err != nil {
		t.Fatalf("ParsePortSpecs() unexpected error = %v", err)
	}

	socket := func(protocol, ip string, port int) hostSocket {
		return hostSocket{Protocol: protocol, IP: net.ParseIP(ip), Port: port}
	}
	appContainer := func(app string, ports ...container.Port) container.Summary {
		return container.Summary{Labels: map[string]string{config.LabelAppName: app}, Ports: ports}
	}

	tests := []struct {
		name    string
		sockets []hostSocket
		running []container.Summary
		wantErr string
	}{
		{
			name:    "sockets on other ports",
			sockets: []hostSocket{socket("tcp", "0.0.0.0", 22), socket("tcp", "127.0.0.1", 9922)},
		},
		{
			name:    "listener on all interfaces",
			sockets: []hostSocket{socket("tcp", "0.0.0.0", 8890)},
			wantErr: "host port 8890/tcp from '8890:8890' is already in use on the server (a process is listening on 0.0.0.0:8890/tcp)",
		},
		{
			name:    "same port on the other protocol",
			sockets: []hostSocket{socket("udp", "0.0.0.0", 8890)},
		},
		{
			name:    "udp socket inside the published range",
			sockets: []hostSocket{socket("udp", "::", 49200)},
			wantErr: "host port 49200/udp from '49152-49351:49152-49351/udp' is already in use",
		},
		{
			name:    "listener on a different specific ip",
			sockets: []hostSocket{socket("tcp", "127.0.0.1", 5432)},
		},
		{
			name:    "listener on all interfaces overlaps a specific ip",
			sockets: []hostSocket{socket("tcp", "::", 5432)},
			wantErr: "host port 10.0.0.2:5432/tcp from '10.0.0.2:5432:5432' is already in use",
		},
		{
			name:    "docker-proxy of the app's current container",
			sockets: []hostSocket{socket("tcp", "0.0.0.0", 8890), socket("tcp", "::", 8890), socket("udp", "0.0.0.0", 49200)},
			running: []container.Summary{appContainer("noray",
				container.Port{IP: "0.0.0.0", PrivatePort: 8890, PublicPort: 8890, Type: "tcp"},
				container.Port{IP: "::", PrivatePort: 8890, PublicPort: 8890, Type: "tcp"},
				container.Port{IP: "0.0.0.0", PrivatePort: 49200, PublicPort: 49200, Type: "udp"},
			)},
		},
		{
			name:    "the app's container publishes the port on another ip",
			sockets: []hostSocket{socket("tcp", "0.0.0.0", 8890)},
			running: []container.Summary{appContainer("noray",
				container.Port{IP: "127.0.0.1", PrivatePort: 8890, PublicPort: 8890, Type: "tcp"},
			)},
			wantErr: "host port 8890/tcp from '8890:8890' is already in use",
		},
		{
			name:    "another app's container does not exempt the socket",
			sockets: []hostSocket{socket("tcp", "0.0.0.0", 8890)},
			running: []container.Summary{appContainer("web",
				container.Port{IP: "0.0.0.0", PrivatePort: 8890, PublicPort: 8890, Type: "tcp"},
			)},
			wantErr: "host port 8890/tcp from '8890:8890' is already in use",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := findHostSocketConflict(published, tt.sockets, tt.running, "noray")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("findHostSocketConflict() unexpected error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("findHostSocketConflict() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestUsesHostNetwork(t *testing.T) {
	hostNetworked := container.Summary{Labels: map[string]string{config.LabelAppName: "noray"}}
	hostNetworked.HostConfig.NetworkMode = "host"
	bridged := container.Summary{Labels: map[string]string{config.LabelAppName: "noray"}}
	bridged.HostConfig.NetworkMode = "haloy"

	if !usesHostNetwork([]container.Summary{hostNetworked}, "noray") {
		t.Error("usesHostNetwork() = false for the app's host-network container, want true")
	}
	if usesHostNetwork([]container.Summary{bridged}, "noray") {
		t.Error("usesHostNetwork() = true for a bridged container, want false")
	}
	if usesHostNetwork([]container.Summary{hostNetworked}, "web") {
		t.Error("usesHostNetwork() = true for another app's host-network container, want false")
	}
}
