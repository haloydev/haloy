package api

import (
	"testing"
)

func TestGetClientIP_HostPort(t *testing.T) {
	got := getClientIP("198.51.100.7:12345")
	if got != "198.51.100.7" {
		t.Fatalf("getClientIP() = %q, want %q", got, "198.51.100.7")
	}
}

func TestGetClientIP_RawRemoteAddr(t *testing.T) {
	got := getClientIP("198.51.100.7")
	if got != "198.51.100.7" {
		t.Fatalf("getClientIP() = %q, want %q", got, "198.51.100.7")
	}
}

func TestGetClientIP_IPv6HostPort(t *testing.T) {
	got := getClientIP("[::1]:12345")
	if got != "::1" {
		t.Fatalf("getClientIP() = %q, want %q", got, "::1")
	}
}
