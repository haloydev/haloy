package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"
)

// PublishedPort is one host port bound to one container port, parsed from a
// Docker-style port spec in the target's ports list.
type PublishedPort struct {
	Raw           string
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string // "tcp" or "udp"
}

// reservedHostPorts are TCP host ports Haloy itself binds on every server.
var reservedHostPorts = map[int]string{
	80:   "haloy-proxy (HTTP)",
	443:  "haloy-proxy (HTTPS)",
	8080: "haloyd's ACME challenge listener on 127.0.0.1",
	9922: "haloyd's API on 127.0.0.1",
}

// ParsePortSpec parses a Docker-style port spec,
// [host_ip:]host_port:container_port[/tcp|udp], where both ports can be a
// range of equal length. A range expands to one PublishedPort per port.
func ParsePortSpec(raw string) ([]PublishedPort, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return nil, fmt.Errorf("invalid port mapping '%s'; expected '[host_ip:]host_port:container_port[/tcp|udp]'", raw)
	}

	_, portPart := nat.SplitProtoPort(spec)
	if !strings.Contains(portPart, ":") {
		return nil, fmt.Errorf("port mapping '%s' has no host port; use 'host_port:container_port', e.g. '%s:%s'", raw, portPart, spec)
	}

	mappings, err := nat.ParsePortSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid port mapping '%s': %w", raw, err)
	}

	ports := make([]PublishedPort, 0, len(mappings))
	for _, m := range mappings {
		proto := m.Port.Proto()
		if proto != "tcp" && proto != "udp" {
			return nil, fmt.Errorf("invalid port mapping '%s': protocol must be 'tcp' or 'udp', got '%s'", raw, proto)
		}

		hostPortStr := m.Binding.HostPort
		if strings.Contains(hostPortStr, "-") {
			return nil, fmt.Errorf("invalid port mapping '%s': host and container ranges must be the same length", raw)
		}
		hostPort, err := strconv.Atoi(hostPortStr)
		if err != nil || hostPort < 1 || hostPort > 65535 {
			return nil, fmt.Errorf("invalid port mapping '%s': host port must be between 1 and 65535", raw)
		}
		containerPort := m.Port.Int()
		if containerPort < 1 || containerPort > 65535 {
			return nil, fmt.Errorf("invalid port mapping '%s': container port must be between 1 and 65535", raw)
		}

		if proto == "tcp" {
			if owner, reserved := reservedHostPorts[hostPort]; reserved {
				return nil, fmt.Errorf("invalid port mapping '%s': host port %d/tcp is used by %s", raw, hostPort, owner)
			}
		}

		ports = append(ports, PublishedPort{
			Raw:           raw,
			HostIP:        m.Binding.HostIP,
			HostPort:      hostPort,
			ContainerPort: containerPort,
			Protocol:      proto,
		})
	}
	return ports, nil
}

// ParsePortSpecs parses every spec and rejects two mappings that would bind
// the same host port.
func ParsePortSpecs(specs []string) ([]PublishedPort, error) {
	var all []PublishedPort
	for _, spec := range specs {
		ports, err := ParsePortSpec(spec)
		if err != nil {
			return nil, err
		}
		for _, p := range ports {
			for _, existing := range all {
				if p.Overlaps(existing) {
					return nil, fmt.Errorf("port mappings '%s' and '%s' both bind host port %s", existing.Raw, p.Raw, p.HostAddr())
				}
			}
			all = append(all, p)
		}
	}
	return all, nil
}

// Overlaps reports whether p and other would bind the same host socket:
// same protocol and host port, and host IPs that are equal or unspecified.
func (p PublishedPort) Overlaps(other PublishedPort) bool {
	if p.Protocol != other.Protocol || p.HostPort != other.HostPort {
		return false
	}
	return HostIPsOverlap(p.HostIP, other.HostIP)
}

// HostAddr formats the host side of the mapping, e.g. "127.0.0.1:8891/tcp".
func (p PublishedPort) HostAddr() string {
	port := strconv.Itoa(p.HostPort) + "/" + p.Protocol
	if p.HostIP == "" {
		return port
	}
	return net.JoinHostPort(p.HostIP, port)
}

// HostIPsOverlap reports whether two host IPs of a port binding can collide.
// An empty or unspecified address (0.0.0.0, ::) binds every interface.
func HostIPsOverlap(a, b string) bool {
	if isUnspecifiedIP(a) || isUnspecifiedIP(b) {
		return true
	}
	ipA, ipB := net.ParseIP(a), net.ParseIP(b)
	if ipA == nil || ipB == nil {
		return a == b
	}
	return ipA.Equal(ipB)
}

func isUnspecifiedIP(ip string) bool {
	if ip == "" {
		return true
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsUnspecified()
}
