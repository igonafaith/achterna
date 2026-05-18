package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/igonafaith/achterna/internal/cache"
	"github.com/igonafaith/achterna/internal/circuit"
)

// metricsRecorder is the minimal metrics interface needed by middlewares.
type metricsRecorder interface {
	IncRateLimited()
	IncCircuitOpen()
}

// circuitResponseRecorder captures status code so the circuit can record it.
type circuitResponseRecorder struct {
	http.ResponseWriter
	status int
}

func (cr *circuitResponseRecorder) WriteHeader(code int) {
	cr.status = code
	cr.ResponseWriter.WriteHeader(code)
}

// CircuitBreak returns 503 when the circuit is open.
// If a stale cache entry exists, it serves that instead.
func CircuitBreak(cb *circuit.CircuitBreaker, c cache.Cache, m metricsRecorder) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set current circuit state before any response is written
			w.Header().Set("X-Circuit", cb.State().String())

			if !cb.Allow() {
				if m != nil {
					m.IncCircuitOpen()
				}

				// Serve stale cache entry if available (stale-if-error)
				key := cache.DeriveKey(r)
				if entry, ok := c.GetStale(key); ok {
					w.Header().Set("X-Cache", "STALE")
					copyHeader(w.Header(), entry.Header)
					w.WriteHeader(entry.StatusCode)
					w.Write(entry.Body) //nolint:errcheck
					return
				}

				retryAfter := fmt.Sprintf("%d", int(cb.RetryAfter().Seconds()))
				w.Header().Set("Retry-After", retryAfter)
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}

			rec := &circuitResponseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			cb.RecordResult(rec.status)
		})
	}
}

// Metrics middleware increments counters and tracks latency.
func Metrics(m interface {
	IncTotal()
	IncActive()
	DecActive()
	IncError()
	IncStatus(int)
	RecordLatency(time.Duration)
}) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.IncTotal()
			m.IncActive()
			defer m.DecActive()

			start := time.Now()
			rec := &circuitResponseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			elapsed := time.Since(start)
			m.RecordLatency(elapsed)
			m.IncStatus(rec.status)

			if rec.status >= 500 {
				m.IncError()
			}
		})
	}
}
