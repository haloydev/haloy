package proxy

import (
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// isWebSocketUpgrade checks if the request is a WebSocket upgrade request.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// handleWebSocket handles WebSocket upgrade requests by hijacking the connection
// and creating a bidirectional tunnel to the backend.
func (p *Proxy) handleWebSocket(w http.ResponseWriter, r *http.Request, route *Route, startTime time.Time) {
	if len(route.Backends) == 0 {
		p.logRequest(r, http.StatusBadGateway, time.Since(startTime))
		p.serveErrorPage(w, http.StatusBadGateway, "No backends available")
		return
	}

	backend := p.selectBackend(route)
	backendAddr := net.JoinHostPort(backend.IP, backend.Port)

	backendConn, err := net.DialTimeout("tcp", backendAddr, 10*time.Second)
	if err != nil {
		p.logger.Error("WebSocket: failed to connect to backend",
			"backend", backendAddr,
			"error", err)
		p.logRequest(r, http.StatusBadGateway, time.Since(startTime))
		p.serveErrorPage(w, http.StatusBadGateway, "Backend unavailable")
		return
	}
	defer backendConn.Close()

	// Hijack the client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		p.logger.Error("WebSocket: hijacking not supported")
		p.logRequest(r, http.StatusInternalServerError, time.Since(startTime))
		http.Error(w, "WebSocket not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		p.logger.Error("WebSocket: failed to hijack connection", "error", err)
		p.logRequest(r, http.StatusInternalServerError, time.Since(startTime))
		return
	}
	defer clientConn.Close()

	// Forward the original HTTP request to the backend to initiate the WebSocket handshake
	if err := r.Write(backendConn); err != nil {
		p.logger.Error("WebSocket: failed to forward request to backend", "error", err)
		return
	}

	p.logRequest(r, http.StatusSwitchingProtocols, time.Since(startTime))

	// Bidirectional copy between client and backend
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend
	go func() {
		defer wg.Done()
		// First drain any buffered data from the hijacked connection
		if clientBuf.Reader.Buffered() > 0 {
			io.CopyN(backendConn, clientBuf, int64(clientBuf.Reader.Buffered()))
		}
		io.Copy(backendConn, clientConn)
		// Signal EOF to backend
		if tcpConn, ok := backendConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	// Backend -> Client
	go func() {
		defer wg.Done()
		io.Copy(clientConn, backendConn)
		// Signal EOF to client
		if tcpConn, ok := clientConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	wg.Wait()
}
