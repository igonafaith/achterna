package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/igonafaith/achterna/internal/cache"
	"github.com/igonafaith/achterna/internal/circuit"
	"github.com/igonafaith/achterna/internal/health"
	"github.com/igonafaith/achterna/internal/metrics"
)

// configReloader is implemented by the config loader when hot-reload is supported.
type configReloader interface {
	Reload() error
}

type Server struct {
	mux      *http.ServeMux
	logger   *slog.Logger
}

func New(
	cb *circuit.CircuitBreaker,
	c cache.Cache,
	m *metrics.Metrics,
	logger *slog.Logger,
	reloader configReloader,
) *Server {
	s := &Server{mux: http.NewServeMux(), logger: logger}

	healthHandler := health.New(cb, c)
	s.mux.Handle("GET /healthz", healthHandler)
	s.mux.Handle("GET /metrics", m)
	s.mux.HandleFunc("GET /cache/stats", s.cacheStats(c))
	s.mux.HandleFunc("POST /cache/purge", s.cachePurge(c))
	s.mux.HandleFunc("GET /circuit/state", s.circuitState(cb))
	s.mux.HandleFunc("POST /circuit/reset", s.circuitReset(cb))
	if reloader != nil {
		s.mux.HandleFunc("POST /config/reload", s.configReload(reloader))
	}

	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) cacheStats(c cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := c.Stats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

func (s *Server) cachePurge(c cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := r.URL.Query().Get("prefix")
		// DeleteByPrefix("") deletes all entries (empty prefix matches everything).
		c.DeleteByPrefix(prefix)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) circuitState(cb *circuit.CircuitBreaker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"state":                cb.State().String(),
			"consecutive_failures": cb.ConsecutiveFailures(),
			"total_opens":          cb.TotalOpens(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) circuitReset(cb *circuit.CircuitBreaker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cb.Reset()
		s.logger.Info("circuit breaker reset via admin API")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) configReload(reloader configReloader) http.HandlerFunc {
	var inProgress atomic.Bool
	return func(w http.ResponseWriter, r *http.Request) {
		if !inProgress.CompareAndSwap(false, true) {
			http.Error(w, "reload already in progress", http.StatusConflict)
			return
		}
		defer inProgress.Store(false)

		if err := reloader.Reload(); err != nil {
			s.logger.Error("config reload failed", slog.Any("error", err))
			http.Error(w, "reload failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.logger.Info("config reloaded via admin API")
		w.WriteHeader(http.StatusNoContent)
	}
}
