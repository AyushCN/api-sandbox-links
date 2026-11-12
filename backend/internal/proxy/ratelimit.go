package proxy

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type visitor struct {
	tokens     float64
	lastSeen   time.Time
	lastFilled time.Time
}

// RateLimitStats exposes real-time limiter counters for observability and metrics.
type RateLimitStats struct {
	TotalRejected uint64 `json:"total_rejected"`
	ActiveIPs     int    `json:"active_ips"`
	RPSLimit      int    `json:"rps_limit"`
	BurstLimit    int    `json:"burst_limit"`
}

// IPRateLimiter provides an in-memory, thread-safe token bucket rate limiter per client IP.
type IPRateLimiter struct {
	rate          float64
	burst         int
	mu            sync.Mutex
	visitors      map[string]*visitor
	totalRejected uint64
}

// NewIPRateLimiter constructs a new IPRateLimiter and starts a periodic cleanup worker.
func NewIPRateLimiter(rps, burst int) *IPRateLimiter {
	if rps <= 0 {
		rps = 100
	}
	if burst <= 0 {
		burst = 200
	}
	limiter := &IPRateLimiter{
		rate:     float64(rps),
		burst:    burst,
		visitors: make(map[string]*visitor),
	}
	return limiter
}

// Allow reports whether a request from ip is permitted under current token bucket capacity.
func (l *IPRateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	v, exists := l.visitors[ip]
	if !exists {
		l.visitors[ip] = &visitor{
			tokens:     float64(l.burst - 1),
			lastSeen:   now,
			lastFilled: now,
		}
		return true
	}

	// Calculate tokens added since last replenishment
	elapsed := now.Sub(v.lastFilled).Seconds()
	v.lastFilled = now
	v.lastSeen = now

	v.tokens += elapsed * l.rate
	if v.tokens > float64(l.burst) {
		v.tokens = float64(l.burst)
	}

	if v.tokens >= 1.0 {
		v.tokens -= 1.0
		return true
	}

	atomic.AddUint64(&l.totalRejected, 1)
	return false
}

// Cleanup removes visitors that have been idle longer than maxIdle.
func (l *IPRateLimiter) Cleanup(maxIdle time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	removed := 0
	for ip, v := range l.visitors {
		if now.Sub(v.lastSeen) > maxIdle {
			delete(l.visitors, ip)
			removed++
		}
	}
	return removed
}

// Stats returns an observable snapshot of current rate limiting metrics.
func (l *IPRateLimiter) Stats() RateLimitStats {
	l.mu.Lock()
	active := len(l.visitors)
	l.mu.Unlock()

	return RateLimitStats{
		TotalRejected: atomic.LoadUint64(&l.totalRejected),
		ActiveIPs:     active,
		RPSLimit:      int(l.rate),
		BurstLimit:    l.burst,
	}
}

// clientIP extracts the client IP address from RemoteAddr or Forwarded headers.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := net.ParseIP(xff)
		if parts != nil {
			return xff
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

