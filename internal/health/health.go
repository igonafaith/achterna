package health

import (
	"encoding/json"
	"net/http"

	"github.com/igonafaith/achterna/internal/cache"
	"github.com/igonafaith/achterna/internal/circuit"
)

type Health struct {
	cb    *circuit.CircuitBreaker
	cache cache.Cache
}

func New(cb *circuit.CircuitBreaker, c cache.Cache) *Health {
	return &Health{cb: cb, cache: c}
}

type response struct {
	Status  string `json:"status"`  // "healthy" | "degraded" | "unhealthy"
	Circuit string `json:"circuit"` // "closed" | "open" | "half-open"
	Cache   string `json:"cache"`   // "ok" | "high_usage"
}

func (h *Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	state := h.cb.State()

	resp := response{
		Status:  "healthy",
		Circuit: state.String(),
		Cache:   "ok",
	}

	switch state {
	case circuit.StateOpen:
		resp.Status = "degraded"
	case circuit.StateHalfOpen:
		resp.Status = "degraded"
	}

	stats := h.cache.Stats()
	if stats.MaxBytes > 0 && float64(stats.SizeBytes)/float64(stats.MaxBytes) > 0.9 {
		resp.Cache = "high_usage"
	}

	statusCode := http.StatusOK
	if resp.Status != "healthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}
