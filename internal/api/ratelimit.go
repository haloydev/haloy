package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen int64 // UnixNano, accessed atomically
}
type RateLimiter struct {
	mu       sync.RWMutex
	visitors map[string]*visitor
	r        rate.Limit
	b        int
}

func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		r:        r,
		b:        b,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) getVisitor(ip string) *rate.Limiter {
	rl.mu.RLock()
	v, exists := rl.visitors[ip]
	rl.mu.RUnlock()
	if exists {
		// Thread-safe update
		atomic.StoreInt64(&v.lastSeen, time.Now().UnixNano())
		return v.limiter
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if v, exists = rl.visitors[ip]; exists {
		atomic.StoreInt64(&v.lastSeen, time.Now().UnixNano())
		return v.limiter
	}
	limiter := rate.NewLimiter(rl.r, rl.b)
	rl.visitors[ip] = &visitor{
		limiter:  limiter,
		lastSeen: time.Now().UnixNano(),
	}
	return limiter
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			// Thread-safe read
			lastSeen := time.Unix(0, atomic.LoadInt64(&v.lastSeen))
			if time.Since(lastSeen) > 3*time.Minute {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func getClientIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return ip
	}
	return remoteAddr
}

// clientIP returns the IP to rate limit on. API requests arrive via the haloy
// proxy on loopback with the real client in X-Forwarded-For; the last entry
// is the one appended by the proxy, so it cannot be spoofed by the client.
// Non-loopback connections are keyed on their remote address directly.
func clientIP(r *http.Request) string {
	ip := getClientIP(r.RemoteAddr)
	parsed := net.ParseIP(ip)
	if parsed == nil || !parsed.IsLoopback() {
		return ip
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return ip
	}
	hops := strings.Split(xff, ",")
	if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
		return last
	}
	return ip
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		limiter := rl.getVisitor(ip)
		if !limiter.Allow() {
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
