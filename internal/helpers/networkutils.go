package helpers

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// GetARecord returns the first A record (IPv4 address) for the provided host.
// It returns an error if no A record is found.
func GetARecord(host string) (net.IP, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4, nil
		}
	}
	return nil, fmt.Errorf("no A record found for host: %s", host)
}

// GetExternalIP queries a public service for this machine's external IPv4.
// It returns the IP or an error.
func GetExternalIP() (net.IP, error) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return nil, fmt.Errorf("failed to query external IP service: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read external IP response: %w", err)
	}

	ipStr := strings.TrimSpace(string(body))
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 address returned: %s", ipStr)
	}
	return ip, nil
}
