package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/igonafaith/achterna/internal/cache"
	"github.com/igonafaith/achterna/internal/circuit"
	"github.com/igonafaith/achterna/internal/config"
	"github.com/igonafaith/achterna/internal/metrics"
	"github.com/igonafaith/achterna/internal/middleware"
	"github.com/igonafaith/achterna/internal/ratelimit"
	"github.com/igonafaith/achterna/test/testutil"
)

func buildStack(origin *httptest.Server) (http.Handler, *cache.MemoryCache, *circuit.CircuitBreaker, *ratelimit.Limiter) {
	c := cache.New(64)
	cb := circuit.New(circuit.Config{
		FailureThreshold:   3,
		SuccessThreshold:   2,
		Timeout:            200 * time.Millisecond,
		CountedStatusCodes: []int{502, 503, 504},
	})
	rl := ratelimit.New(1000, 100)
	m := metrics.New()

	cacheCfg := config.CacheConfig{
		Enabled:             true,
		DefaultTTL:          5 * time.Minute,
		CacheableExtensions: []string{".css", ".js", ".png"},
		BypassMethods:       []string{"POST", "PUT", "PATCH", "DELETE"},
	}

	// Simple handler that forwards to the test origin
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(origin.URL + r.URL.Path)
		if err != nil {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
	})

	handler := middleware.Chain(
		proxyHandler,
		middleware.CircuitBreak(cb, c, m),
		middleware.CacheCheck(c, cacheCfg),
		middleware.RateLimit(rl, m),
	)

	return handler, c, cb, rl
}

// --- Tests ---

func TestNormalProxy(t *testing.T) {
	origin := testutil.NewMockOrigin()
	defer origin.Close()

	handler, _, _, rl := buildStack(origin)
	defer rl.Close()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCacheHit(t *testing.T) {
	originHits := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte("body{}"))
	}))
	defer origin.Close()

	handler, _, _, rl := buildStack(origin)
	defer rl.Close()

	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/style.css", nil)
		r.Host = "example.com"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	if originHits > 1 {
		t.Fatalf("cache should reduce origin calls; got %d hits", originHits)
	}
}

func TestCacheBypassPOST(t *testing.T) {
	originHits := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.Write([]byte("ok"))
	}))
	defer origin.Close()

	handler, _, _, rl := buildStack(origin)
	defer rl.Close()

	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/submit", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	if originHits != 3 {
		t.Fatalf("POST should never be cached; expected 3 origin hits, got %d", originHits)
	}
}

func TestCircuitTrip(t *testing.T) {
	origin := testutil.NewMockOrigin()
	defer origin.Close()

	handler, _, cb, rl := buildStack(origin)
	defer rl.Close()

	// Hit /fail until circuit trips (threshold=3)
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/fail", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	if cb.State() != circuit.StateOpen {
		t.Fatalf("circuit should be open after %d failures, got %s", 3, cb.State())
	}

	// Next request should be 503 without hitting origin
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with open circuit, got %d", w.Code)
	}
}

func TestCircuitRecovery(t *testing.T) {
	origin := testutil.NewMockOrigin()
	defer origin.Close()

	handler, _, cb, rl := buildStack(origin)
	defer rl.Close()

	// Trip the circuit
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/fail", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	// Wait for timeout → half-open
	time.Sleep(300 * time.Millisecond)

	// Send successful requests to close
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	if cb.State() != circuit.StateClosed {
		t.Fatalf("circuit should be closed after recovery, got %s", cb.State())
	}
}

func TestRateLimit(t *testing.T) {
	origin := testutil.NewMockOrigin()
	defer origin.Close()

	c := cache.New(64)
	cb := circuit.New(circuit.Config{FailureThreshold: 100, SuccessThreshold: 1, Timeout: time.Minute})
	rl := ratelimit.New(1, 2) // 1 rps, burst 2
	m := metrics.New()
	defer rl.Close()

	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Chain(
		proxyHandler,
		middleware.CircuitBreak(cb, c, m),
		middleware.RateLimit(rl, m),
	)

	rejected := 0
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			rejected++
		}
	}

	if rejected == 0 {
		t.Fatal("expected some requests to be rate limited")
	}
}

func TestStaleCache(t *testing.T) {
	// Serve a stale cached entry when circuit is open
	c := cache.New(64)
	cb := circuit.New(circuit.Config{
		FailureThreshold:   1,
		SuccessThreshold:   1,
		Timeout:            time.Minute,
		CountedStatusCodes: []int{502},
	})
	m := metrics.New()

	// Pre-populate cache with stale entry
	key := "GET:example.com/style.css?"
	c.Set(key, &cache.Entry{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/css"}},
		Body:       []byte("body{}"),
		ExpiresAt:  time.Now().Add(-1 * time.Minute), // already expired
	})

	// Trip the circuit
	cb.RecordResult(502)

	failHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	cacheCfg := config.CacheConfig{
		Enabled:             true,
		DefaultTTL:          5 * time.Minute,
		CacheableExtensions: []string{".css"},
	}
	handler := middleware.Chain(
		failHandler,
		middleware.CircuitBreak(cb, c, m),
		middleware.CacheCheck(c, cacheCfg),
	)

	r := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	r.Host = "example.com"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Cache") != "STALE" {
		t.Fatalf("expected X-Cache: STALE, got %q", w.Header().Get("X-Cache"))
	}
}
