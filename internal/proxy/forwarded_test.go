package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func newTestProxy() *Proxy {
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func TestProxyToBackend_ForwardedHeaders(t *testing.T) {
	received := make(chan http.Header, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	backendHost, backendPort, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	p := newTestProxy()
	route := &Route{
		Canonical: "example.com",
		Backends:  []Backend{{IP: backendHost, Port: backendPort}},
	}

	r := httptest.NewRequest(http.MethodGet, "https://example.com/path", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Forwarded-Host", "spoofed.example.com")
	r.Header.Set("X-Forwarded-Proto", "spoofed")
	r.Header.Set("X-Real-IP", "5.6.7.8")
	r.Header.Set("Forwarded", "for=1.2.3.4")

	w := httptest.NewRecorder()
	p.proxyToBackend(w, r, route, time.Now())

	var headers http.Header
	select {
	case headers = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("backend did not receive request")
	}

	// httptest.NewRequest sets RemoteAddr to 192.0.2.1:1234
	if got := headers.Get("X-Forwarded-For"); got != "192.0.2.1" {
		t.Errorf("X-Forwarded-For = %q, want %q (spoofed value must be stripped)", got, "192.0.2.1")
	}
	if got := headers.Get("X-Forwarded-Host"); got != "example.com" {
		t.Errorf("X-Forwarded-Host = %q, want %q", got, "example.com")
	}
	if got := headers.Get("X-Forwarded-Proto"); got != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want %q", got, "https")
	}
	if got := headers.Get("X-Real-IP"); got != "" {
		t.Errorf("X-Real-IP = %q, want it stripped", got)
	}
	if got := headers.Get("Forwarded"); got != "" {
		t.Errorf("Forwarded = %q, want it stripped", got)
	}
}

func TestSetForwardedHeaders(t *testing.T) {
	tests := []struct {
		name      string
		tls       *tls.ConnectionState
		wantProto string
	}{
		{name: "https request", tls: &tls.ConnectionState{}, wantProto: "https"},
		{name: "http request", tls: nil, wantProto: "http"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			r.TLS = tt.tls
			r.RemoteAddr = "203.0.113.7:54321"
			r.Header.Set("X-Forwarded-For", "1.2.3.4")
			r.Header.Set("X-Forwarded-Host", "spoofed.example.com")
			r.Header.Set("X-Forwarded-Proto", "spoofed")
			r.Header.Set("X-Real-IP", "5.6.7.8")
			r.Header.Set("Forwarded", "for=1.2.3.4")

			setForwardedHeaders(r)

			if got := r.Header.Get("X-Forwarded-For"); got != "203.0.113.7" {
				t.Errorf("X-Forwarded-For = %q, want %q", got, "203.0.113.7")
			}
			if got := r.Header.Get("X-Forwarded-Host"); got != "example.com" {
				t.Errorf("X-Forwarded-Host = %q, want %q", got, "example.com")
			}
			if got := r.Header.Get("X-Forwarded-Proto"); got != tt.wantProto {
				t.Errorf("X-Forwarded-Proto = %q, want %q", got, tt.wantProto)
			}
			if got := r.Header.Get("X-Real-IP"); got != "" {
				t.Errorf("X-Real-IP = %q, want it stripped", got)
			}
			if got := r.Header.Get("Forwarded"); got != "" {
				t.Errorf("Forwarded = %q, want it stripped", got)
			}
		})
	}
}

func TestHandleWebSocket_ForwardedHeaders(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backendListener.Close()

	received := make(chan http.Header, 1)
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		received <- req.Header.Clone()
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
	}()

	backendHost, backendPort, err := net.SplitHostPort(backendListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	p := newTestProxy()
	route := &Route{
		Canonical: "example.com",
		Backends:  []Backend{{IP: backendHost, Port: backendPort}},
	}

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.handleWebSocket(w, r, route, time.Now())
	}))
	defer front.Close()

	frontURL, err := url.Parse(front.URL)
	if err != nil {
		t.Fatal(err)
	}

	clientConn, err := net.Dial("tcp", frontURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	handshake := "GET / HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"X-Forwarded-For: 1.2.3.4\r\n" +
		"X-Forwarded-Host: spoofed.example.com\r\n" +
		"X-Forwarded-Proto: spoofed\r\n" +
		"X-Real-IP: 5.6.7.8\r\n" +
		"Forwarded: for=1.2.3.4\r\n" +
		"\r\n"
	if _, err := clientConn.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}

	var headers http.Header
	select {
	case headers = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("backend did not receive websocket handshake")
	}

	if got := headers.Get("X-Forwarded-For"); got != "127.0.0.1" {
		t.Errorf("X-Forwarded-For = %q, want %q (spoofed value must be stripped)", got, "127.0.0.1")
	}
	if got := headers.Get("X-Forwarded-Host"); got != "example.com" {
		t.Errorf("X-Forwarded-Host = %q, want %q", got, "example.com")
	}
	if got := headers.Get("X-Forwarded-Proto"); got != "http" {
		t.Errorf("X-Forwarded-Proto = %q, want %q", got, "http")
	}
	if got := headers.Get("X-Real-IP"); got != "" {
		t.Errorf("X-Real-IP = %q, want it stripped", got)
	}
	if got := headers.Get("Forwarded"); got != "" {
		t.Errorf("Forwarded = %q, want it stripped", got)
	}
}
