package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

type clientVisitor struct {
	tokens     float64
	lastSeen   time.Time
	lastUpdate time.Time
}

// RateLimiter tracks request rate limits per IP address using a token bucket.
type RateLimiter struct {
	mu          sync.Mutex
	visitors    map[string]*clientVisitor
	rate        float64       // tokens added per second
	burst       float64       // maximum token capacity
	ttl         time.Duration // idle visitor cleanup TTL
	stopCleanup chan struct{}
}

// NewRateLimiter initializes a new RateLimiter.
// limit: number of allowed requests within interval duration.
// interval: period over which limit applies.
// burst: maximum burst capacity allowed.
func NewRateLimiter(limit float64, interval time.Duration, burst float64) *RateLimiter {
	rate := limit / interval.Seconds()
	rl := &RateLimiter{
		visitors:    make(map[string]*clientVisitor),
		rate:        rate,
		burst:       burst,
		ttl:         10 * time.Minute,
		stopCleanup: make(chan struct{}),
	}

	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, v := range rl.visitors {
				if now.Sub(v.lastSeen) > rl.ttl {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			return
		}
	}
}

// Close stops the background cleanup goroutine.
func (rl *RateLimiter) Close() {
	select {
	case <-rl.stopCleanup:
	default:
		close(rl.stopCleanup)
	}
}

// getIP extracts client IP address from http.Request, handling headers like X-Forwarded-For.
func getIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		for _, ipStr := range splitAndTrim(forwarded, ",") {
			if ip := net.ParseIP(ipStr); ip != nil {
				return ip.String()
			}
		}
	}

	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		if ip := net.ParseIP(realIP); ip != nil {
			return ip.String()
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitAndTrim(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			item := trimSpace(s[start:i])
			if item != "" {
				result = append(result, item)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		item := trimSpace(s[start:])
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// Allow checks if a request from client IP is allowed.
func (rl *RateLimiter) Allow(ip string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, exists := rl.visitors[ip]
	if !exists {
		v = &clientVisitor{
			tokens:     rl.burst - 1,
			lastSeen:   now,
			lastUpdate: now,
		}
		rl.visitors[ip] = v
		return true, 0
	}

	// Calculate refilled tokens since last update
	elapsed := now.Sub(v.lastUpdate).Seconds()
	v.tokens += elapsed * rl.rate
	if v.tokens > rl.burst {
		v.tokens = rl.burst
	}
	v.lastUpdate = now
	v.lastSeen = now

	if v.tokens >= 1 {
		v.tokens -= 1
		return true, 0
	}

	// Calculate wait time until next token is available
	needed := 1.0 - v.tokens
	retryAfterSecs := needed / rl.rate
	return false, time.Duration(retryAfterSecs * float64(time.Second))
}

// LimitFunc wraps http.HandlerFunc with rate limiting.
func (rl *RateLimiter) LimitFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getIP(r)
		allowed, _ := rl.Allow(ip)
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Too many request attempts. Please wait before trying again.",
			})
			return
		}
		next(w, r)
	}
}

// DefaultAuthRateLimiter provides a standard rate limiter for authentication routes:
// Allows 5 requests per 1 minute per IP with a burst capacity of 5.
var DefaultAuthRateLimiter = NewRateLimiter(5, time.Minute, 5)

// AuthRateLimit wraps an http.HandlerFunc with default auth rate limiting.
func AuthRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return DefaultAuthRateLimiter.LimitFunc(next)
}
