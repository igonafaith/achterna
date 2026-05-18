package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/igonafaith/achterna/internal/ctxkey"
)

// responseRecorder wraps ResponseWriter to capture status code and bytes written.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.status = code
	rr.ResponseWriter.WriteHeader(code)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	n, err := rr.ResponseWriter.Write(b)
	rr.bytes += n
	return n, err
}

// Logger writes a structured access log entry after each request.
func Logger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rr := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rr, r)

			logger.Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
				slog.Int("status", rr.status),
				slog.Int("bytes", rr.bytes),
				slog.Duration("latency", time.Since(start)),
				slog.String("request_id", ctxkey.RequestID(r.Context())),
			)
		})
	}
}
