package docker

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/haloydev/haloy/internal/config"
)

// TCP states in /proc/net/tcp and /proc/net/tcp6.
const (
	tcpTimeWait = 0x06
	tcpClose    = 0x07
	tcpListen   = 0x0A
)

// hostSocket is a socket on the server that holds a local port. On Linux,
// Docker cannot bind a host port that any socket holds: connected client
// sockets and TCP sockets in TIME_WAIT included, unless the socket set
// SO_REUSEADDR, which /proc/net does not show.
type hostSocket struct {
	Protocol string // "tcp" or "udp"
	IP       net.IP
	Port     int
	// Listening is set for a listening TCP socket.
	Listening bool
	// TimeWait is set for a closed TCP connection in TIME_WAIT.
	TimeWait bool
	// Remote is the peer address of a connected socket: a TCP connection or
	// a connected UDP socket. It is empty otherwise.
	Remote string
}

func (s hostSocket) local() string {
	return net.JoinHostPort(s.IP.String(), strconv.Itoa(s.Port)) + "/" + s.Protocol
}

func (s hostSocket) String() string {
	if s.Remote != "" {
		return s.local() + "->" + s.Remote
	}
	return s.local()
}

// holder describes what holds the port, for error messages.
func (s hostSocket) holder() string {
	switch {
	case s.Listening:
		return "a process is listening on " + s.local()
	case s.TimeWait:
		return fmt.Sprintf("a recently closed connection, %s to %s, is in TIME_WAIT; the kernel releases the port within about a minute", s.local(), s.Remote)
	case s.Remote != "":
		return fmt.Sprintf("a connected socket uses it as its local port, %s to %s; this is usually an outgoing client connection that will close on its own", s.local(), s.Remote)
	default:
		return "a process has a socket bound to " + s.local()
	}
}

// transient reports whether the socket is a connection rather than a
// listening or bound socket a process keeps.
func (s hostSocket) transient() bool {
	return s.TimeWait || s.Remote != ""
}

var procNetFiles = []struct{ name, protocol string }{
	{"tcp", "tcp"},
	{"tcp6", "tcp"},
	{"udp", "udp"},
	{"udp6", "udp"},
}

// hostSockets returns the sockets that hold a port on the server. haloyd
// runs as a host service (not in a container), so /proc/net shows the host's
// network namespace. It returns nothing on other operating systems.
func hostSockets() ([]hostSocket, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	return readHostSockets("/proc/net", binary.NativeEndian)
}

// readHostSockets reads the tcp, tcp6, udp and udp6 tables in procNetDir. A
// missing table (tcp6 and udp6 when IPv6 is disabled) is skipped. Unreadable
// tables and malformed lines are reported in the error, alongside every
// socket that could be read.
func readHostSockets(procNetDir string, order binary.ByteOrder) ([]hostSocket, error) {
	var sockets []hostSocket
	var errs []error
	for _, f := range procNetFiles {
		path := filepath.Join(procNetDir, f.name)
		file, err := os.Open(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		parsed, err := parseProcNet(file, f.protocol, order)
		file.Close()
		sockets = append(sockets, parsed...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
		}
	}
	return sockets, errors.Join(errs...)
}

// parseProcNet parses one /proc/net/{tcp,tcp6,udp,udp6} table. Every socket
// bound to a local port counts, including connected client sockets on an
// ephemeral port and TCP connections in TIME_WAIT: on Linux they make Docker's
// bind of that port fail just as a listener does, unless they set
// SO_REUSEADDR, which the table does not show.
func parseProcNet(r io.Reader, protocol string, order binary.ByteOrder) ([]hostSocket, error) {
	var sockets []hostSocket
	var firstErr error
	malformed := 0
	fail := func(lineNum int, format string, a ...any) {
		malformed++
		if firstErr == nil {
			firstErr = fmt.Errorf("line %d: %s", lineNum, fmt.Sprintf(format, a...))
		}
	}

	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] == "sl" {
			continue
		}
		if len(fields) < 4 {
			fail(lineNum, "expected at least 4 fields, got %d", len(fields))
			continue
		}

		state, err := strconv.ParseUint(fields[3], 16, 8)
		if err != nil {
			fail(lineNum, "invalid state %q", fields[3])
			continue
		}
		ip, port, err := parseProcNetAddr(fields[1], order)
		if err != nil {
			fail(lineNum, "invalid local address %q: %v", fields[1], err)
			continue
		}
		if port == 0 {
			continue
		}

		remoteIP, remotePort, err := parseProcNetAddr(fields[2], order)
		if err != nil {
			fail(lineNum, "invalid remote address %q: %v", fields[2], err)
			continue
		}

		socket := hostSocket{Protocol: protocol, IP: ip, Port: port}
		switch {
		case protocol == "tcp" && state == tcpListen:
			socket.Listening = true
		case protocol == "tcp" && state == tcpClose:
			// bound, not connected
		case remotePort != 0 || !remoteIP.IsUnspecified():
			socket.Remote = net.JoinHostPort(remoteIP.String(), strconv.Itoa(remotePort))
			socket.TimeWait = protocol == "tcp" && state == tcpTimeWait
		}
		sockets = append(sockets, socket)
	}

	if err := scanner.Err(); err != nil {
		return sockets, err
	}
	if malformed > 1 {
		return sockets, fmt.Errorf("%w (and %d more malformed lines)", firstErr, malformed-1)
	}
	return sockets, firstErr
}

// parseProcNetAddr parses an "ADDR:PORT" field. The address is hex in 32-bit
// words of host byte order (8 digits for IPv4, 32 for IPv6); the port is hex
// in network order.
func parseProcNetAddr(field string, order binary.ByteOrder) (net.IP, int, error) {
	hexIP, hexPort, ok := strings.Cut(field, ":")
	if !ok {
		return nil, 0, errors.New("missing port")
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid port: %w", err)
	}
	if len(hexIP) != 8 && len(hexIP) != 32 {
		return nil, 0, fmt.Errorf("address has %d hex digits, want 8 or 32", len(hexIP))
	}
	raw, err := hex.DecodeString(hexIP)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid address: %w", err)
	}
	ip := make(net.IP, len(raw))
	for i := 0; i < len(raw); i += 4 {
		order.PutUint32(ip[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	return ip, int(port), nil
}

// findHostSocketConflict returns an error for the first published port that
// a socket on the server already holds. When several sockets hold a port, a
// listening or bound one is named before a connection. Sockets on ports
// the app's own containers publish are skipped: docker-proxy holds those for
// the current deployment, which a replace deploy stops before starting the
// new one.
func findHostSocketConflict(published []config.PublishedPort, sockets []hostSocket, running []container.Summary, appName string) error {
	var own []container.Port
	for _, c := range running {
		if c.Labels[config.LabelAppName] == appName {
			own = append(own, c.Ports...)
		}
	}

	for _, p := range published {
		var held *hostSocket
		for i, s := range sockets {
			if p.Protocol != s.Protocol || p.HostPort != s.Port || !config.HostIPsOverlap(p.HostIP, s.IP.String()) {
				continue
			}
			if publishedByOwnContainer(own, s) {
				continue
			}
			if held == nil || (held.transient() && !s.transient()) {
				held = &sockets[i]
			}
		}
		if held == nil {
			continue
		}
		if held.transient() {
			return fmt.Errorf("host port %s from '%s' is already in use on the server (%s); retry the deploy once the port is released, or publish a different host port", p.HostAddr(), p.Raw, held.holder())
		}
		return fmt.Errorf("host port %s from '%s' is already in use on the server (%s); stop it or publish a different host port", p.HostAddr(), p.Raw, held.holder())
	}
	return nil
}

// publishedByOwnContainer reports whether one of the app's own container
// ports covers the socket. A port published on all interfaces covers every
// socket on that port, connections docker-proxy accepted on a specific local
// address included.
func publishedByOwnContainer(own []container.Port, s hostSocket) bool {
	for _, p := range own {
		if p.Type != s.Protocol || int(p.PublicPort) != s.Port {
			continue
		}
		ip := net.ParseIP(p.IP)
		if p.IP == "" || (ip != nil && ip.IsUnspecified()) {
			return true
		}
		if ip != nil && ip.Equal(s.IP) {
			return true
		}
	}
	return false
}

// usesHostNetwork reports whether a running container of the app shares the
// server's network stack. Its sockets cannot be told apart from other
// processes', so the host socket check would see the app conflict with itself.
func usesHostNetwork(running []container.Summary, appName string) bool {
	for _, c := range running {
		if c.Labels[config.LabelAppName] == appName && container.NetworkMode(c.HostConfig.NetworkMode).IsHost() {
			return true
		}
	}
	return false
}
