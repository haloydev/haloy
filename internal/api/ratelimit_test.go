package api

import (
	"net/http/httptest"
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

func TestClientIP_LoopbackUsesLastForwardedFor(t *testing.T) {
	// Request forwarded by the haloy proxy: client spoofed two entries, the
	// proxy appended the real address last.
	r := httptest.NewRequest("GET", "/v1/version", nil)
	r.RemoteAddr = "127.0.0.1:53422"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 198.51.100.7")

	if got := clientIP(r); got != "198.51.100.7" {
		t.Fatalf("clientIP() = %q, want last X-Forwarded-For hop %q", got, "198.51.100.7")
	}
}

func TestClientIP_LoopbackWithoutForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/version", nil)
	r.RemoteAddr = "127.0.0.1:53422"

	if got := clientIP(r); got != "127.0.0.1" {
		t.Fatalf("clientIP() = %q, want %q", got, "127.0.0.1")
	}
}

func TestClientIP_NonLoopbackIgnoresForwardedFor(t *testing.T) {
	// A direct external connection must not be able to pick its own bucket.
	r := httptest.NewRequest("GET", "/v1/version", nil)
	r.RemoteAddr = "203.0.113.9:44321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("clientIP() = %q, want remote address %q", got, "203.0.113.9")
	}
}
