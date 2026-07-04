package helpers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnyIPMatch(t *testing.T) {
	tests := []struct {
		name string
		a    []net.IP
		b    []net.IP
		want bool
	}{
		{
			name: "match on non-first element",
			a:    []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("5.6.7.8")},
			b:    []net.IP{net.ParseIP("9.9.9.9"), net.ParseIP("5.6.7.8")},
			want: true,
		},
		{
			name: "ipv4 equals ipv4-mapped ipv6",
			a:    []net.IP{net.ParseIP("185.248.146.236").To4()},
			b:    []net.IP{net.ParseIP("::ffff:185.248.146.236")},
			want: true,
		},
		{
			name: "no match",
			a:    []net.IP{net.ParseIP("1.2.3.4")},
			b:    []net.IP{net.ParseIP("4.3.2.1")},
			want: false,
		},
		{
			name: "empty inputs",
			a:    nil,
			b:    []net.IP{net.ParseIP("1.2.3.4")},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyIPMatch(tt.a, tt.b); got != tt.want {
				t.Errorf("AnyIPMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterGlobalUnicastIPs(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("127.0.1.1"),         // loopback (/etc/hosts convention)
		net.ParseIP("::1"),               // ipv6 loopback
		net.ParseIP("169.254.10.10"),     // ipv4 link-local
		net.ParseIP("fe80::1"),           // ipv6 link-local
		net.ParseIP("185.248.146.236"),   // public ipv4
		net.ParseIP("10.0.0.5"),          // private ipv4 (still global unicast)
		net.ParseIP("2a12:6bc0:1337::1"), // public ipv6
	}

	got := FilterGlobalUnicastIPs(ips)

	want := []string{"185.248.146.236", "10.0.0.5", "2a12:6bc0:1337::1"}
	if len(got) != len(want) {
		t.Fatalf("FilterGlobalUnicastIPs() returned %d IPs, want %d: %v", len(got), len(want), got)
	}
	for i, ip := range got {
		if ip.String() != want[i] {
			t.Errorf("FilterGlobalUnicastIPs()[%d] = %s, want %s", i, ip, want[i])
		}
	}
}

func TestResolveDomainDoH(t *testing.T) {
	ctx := context.Background()

	t.Run("parses A records and skips CNAMEs", func(t *testing.T) {
		srv := dohTestServer(t, http.StatusOK,
			`{"Status":0,"Answer":[{"name":"app.example.com","type":5,"data":"origin.example.com."},{"name":"origin.example.com","type":1,"data":"185.248.146.236"},{"name":"origin.example.com","type":1,"data":"185.248.146.129"}]}`)
		defer srv.Close()

		ips, err := resolveDomainDoH(ctx, "app.example.com", []string{srv.URL})
		if err != nil {
			t.Fatalf("resolveDomainDoH() error: %v", err)
		}
		if len(ips) != 2 || ips[0].String() != "185.248.146.236" || ips[1].String() != "185.248.146.129" {
			t.Errorf("resolveDomainDoH() = %v, want [185.248.146.236 185.248.146.129]", ips)
		}
	})

	t.Run("empty answer returns no IPs without error", func(t *testing.T) {
		srv := dohTestServer(t, http.StatusOK, `{"Status":0}`)
		defer srv.Close()

		ips, err := resolveDomainDoH(ctx, "ipv6only.example.com", []string{srv.URL})
		if err != nil {
			t.Fatalf("resolveDomainDoH() error: %v", err)
		}
		if len(ips) != 0 {
			t.Errorf("resolveDomainDoH() = %v, want empty", ips)
		}
	})

	t.Run("falls back to second provider", func(t *testing.T) {
		failing := dohTestServer(t, http.StatusInternalServerError, "")
		defer failing.Close()
		working := dohTestServer(t, http.StatusOK,
			`{"Status":0,"Answer":[{"name":"app.example.com","type":1,"data":"1.2.3.4"}]}`)
		defer working.Close()

		ips, err := resolveDomainDoH(ctx, "app.example.com", []string{failing.URL, working.URL})
		if err != nil {
			t.Fatalf("resolveDomainDoH() error: %v", err)
		}
		if len(ips) != 1 || ips[0].String() != "1.2.3.4" {
			t.Errorf("resolveDomainDoH() = %v, want [1.2.3.4]", ips)
		}
	})

	t.Run("errors when all providers fail", func(t *testing.T) {
		malformed := dohTestServer(t, http.StatusOK, `not json`)
		defer malformed.Close()
		failing := dohTestServer(t, http.StatusBadGateway, "")
		defer failing.Close()

		if _, err := resolveDomainDoH(ctx, "app.example.com", []string{malformed.URL, failing.URL}); err == nil {
			t.Error("resolveDomainDoH() expected error, got nil")
		}
	})
}

func dohTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/dns-json" {
			t.Errorf("Accept header = %q, want application/dns-json", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}
