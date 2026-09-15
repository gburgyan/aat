package shop

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitPerMinute is the request budget the shop API reports to each bearer
// token in its RateLimit-Limit header. The sandbox counts requests against it
// but never refuses one, so a batch is never throttled.
const RateLimitPerMinute = 600

// rateLimits counts each bearer token's requests in the current minute.
type rateLimits struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
}

// count records a request by token at now and returns how many requests the
// token has made in now's minute, this one included.
func (l *rateLimits) count(token string, now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if window := now.Truncate(time.Minute); !window.Equal(l.window) {
		l.window = window
		l.counts = map[string]int{}
	}
	l.counts[token]++
	return l.counts[token]
}

// reportRateLimit sets RateLimit-Limit, RateLimit-Remaining, and
// RateLimit-Reset (the seconds left in the minute) on every response, as a
// rate-limited API does. It runs inside requireBearer, which names the token.
func (s *Server) reportRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := s.now()
		token, _ := r.Context().Value(tokenKey{}).(string)
		used := s.limits.count(token, now)
		h := w.Header()
		h.Set("RateLimit-Limit", strconv.Itoa(RateLimitPerMinute))
		h.Set("RateLimit-Remaining", strconv.Itoa(max(RateLimitPerMinute-used, 0)))
		h.Set("RateLimit-Reset", strconv.Itoa(int(now.Truncate(time.Minute).Add(time.Minute).Sub(now).Seconds())))
		next.ServeHTTP(w, r)
	})
}
