package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// networkHTTPClient is used for external IP and DoH lookups so a slow or
// unreachable provider can't stall certificate acquisition.
var networkHTTPClient = &http.Client{Timeout: 5 * time.Second}

// dohProviders are DNS-over-HTTPS endpoints supporting the DNS JSON API.
var dohProviders = []string{
	"https://cloudflare-dns.com/dns-query",
	"https://dns.google/resolve",
}

const dnsTypeA = 1

type dohAnswer struct {
	Type int    `json:"type"`
	Data string `json:"data"`
}

type dohResponse struct {
	Answer []dohAnswer `json:"Answer"`
}

// ResolveDomainDoH looks up the domain's A records via public DNS-over-HTTPS
// providers, bypassing /etc/hosts and local resolver caches so the result
// reflects what public DNS (and ACME validators) see. An empty, non-error
// result means public DNS has no A records for the domain.
func ResolveDomainDoH(ctx context.Context, domain string) ([]net.IP, error) {
	return resolveDomainDoH(ctx, domain, dohProviders)
}

func resolveDomainDoH(ctx context.Context, domain string, providers []string) ([]net.IP, error) {
	var lastErr error
	for _, provider := range providers {
		ips, err := queryDoH(ctx, provider, domain)
		if err != nil {
			lastErr = err
			continue
		}
		return ips, nil
	}
	return nil, fmt.Errorf("all DoH providers failed: %w", lastErr)
}

func queryDoH(ctx context.Context, provider, domain string) ([]net.IP, error) {
	reqURL := fmt.Sprintf("%s?name=%s&type=A", provider, url.QueryEscape(domain))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-json")

	resp, err := networkHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH provider %s returned status %d", provider, resp.StatusCode)
	}

	var parsed dohResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to parse DoH response from %s: %w", provider, err)
	}

	var ips []net.IP
	for _, answer := range parsed.Answer {
		if answer.Type != dnsTypeA {
			continue
		}
		if ip := net.ParseIP(answer.Data); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips, nil
}

// GetLocalIPs returns the global unicast IP addresses (IPv4 and IPv6)
// assigned to this machine's network interfaces.
func GetLocalIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			ips = append(ips, ipNet.IP)
		}
	}
	return FilterGlobalUnicastIPs(ips), nil
}

// FilterGlobalUnicastIPs returns only the global unicast addresses from ips,
// dropping loopback and link-local entries such as /etc/hosts 127.0.1.1
// hostname mappings.
func FilterGlobalUnicastIPs(ips []net.IP) []net.IP {
	var filtered []net.IP
	for _, ip := range ips {
		if ip.IsGlobalUnicast() {
			filtered = append(filtered, ip)
		}
	}
	return filtered
}

// AnyIPMatch reports whether any IP in a equals any IP in b.
func AnyIPMatch(a, b []net.IP) bool {
	for _, ipA := range a {
		for _, ipB := range b {
			if ipA.Equal(ipB) {
				return true
			}
		}
	}
	return false
}

// GetExternalIP queries a public service for this machine's external IPv4.
// It returns the IP or an error.
func GetExternalIP() (net.IP, error) {
	resp, err := networkHTTPClient.Get("https://api.ipify.org?format=text")
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
