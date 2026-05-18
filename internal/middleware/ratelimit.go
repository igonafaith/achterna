package middleware

import (
	"net/http"

	"github.com/igonafaith/achterna/internal/ratelimit"
)

// RateLimit returns 429 when the token bucket is exhausted.
func RateLimit(rl *ratelimit.Limiter, m metricsRecorder) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow() {
				if m != nil {
					m.IncRateLimited()
				}
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
