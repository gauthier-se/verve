package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// loginLimiter throttles login attempts per client IP to blunt password
// brute-forcing (good_practices §8). Each IP gets a token bucket: a small burst
// for honest retries, then a slow sustained refill. Buckets idle past ttl are
// swept opportunistically, so the map cannot grow without bound.
type loginLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// newLoginLimiter allows a burst of 5 attempts, then refills one token every 12
// seconds (~5/min sustained) — generous for a human, punishing for a script.
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Every(12 * time.Second),
		burst:    5,
		ttl:      15 * time.Minute,
	}
}

// allow reports whether an attempt from ip may proceed, consuming a token. A
// refusal also carries how long the caller must wait for the next token: the
// handler turns it into a Retry-After header, so a throttled client can count
// down instead of guessing when to try again.
func (l *loginLimiter) allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for k, v := range l.visitors {
		if now.Sub(v.lastSeen) > l.ttl {
			delete(l.visitors, k)
		}
	}

	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.visitors[ip] = v
	}
	v.lastSeen = now

	// Reserve, not Allow: only a reservation knows the wait until the next token.
	// Cancelling hands the token straight back, so a refused attempt costs the
	// bucket nothing, which is exactly how Allow behaved.
	res := v.limiter.Reserve()
	if !res.OK() {
		return false, l.ttl
	}
	if d := res.Delay(); d > 0 {
		res.Cancel()
		return false, d
	}
	return true, 0
}

// clientIP is the throttling key: the request's remote host, port stripped. It
// deliberately does not trust X-Forwarded-For — that is spoofable until a
// trusted-proxy configuration exists (a forward-auth-era concern, ADR 0008).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
