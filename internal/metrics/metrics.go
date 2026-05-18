package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var latencyBuckets = []time.Duration{
	10 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
}

type Metrics struct {
	TotalRequests  atomic.Int64
	TotalErrors    atomic.Int64
	ActiveRequests atomic.Int64

	statusMu     sync.Mutex
	StatusCounts map[int]*atomic.Int64

	latencyBuckets [6]atomic.Int64 // <10ms,<50ms,<100ms,<500ms,<1s,>=1s
	totalLatencyMs atomic.Int64
	latencyCount   atomic.Int64

	CacheHits    atomic.Int64
	CacheMisses  atomic.Int64
	CircuitOpens atomic.Int64
	RateLimited  atomic.Int64

	Uptime time.Time
}

func New() *Metrics {
	return &Metrics{
		StatusCounts: make(map[int]*atomic.Int64),
		Uptime:       time.Now(),
	}
}

func (m *Metrics) IncTotal()        { m.TotalRequests.Add(1) }
func (m *Metrics) IncActive()       { m.ActiveRequests.Add(1) }
func (m *Metrics) DecActive()       { m.ActiveRequests.Add(-1) }
func (m *Metrics) IncError()        { m.TotalErrors.Add(1) }
func (m *Metrics) IncRateLimited()  { m.RateLimited.Add(1) }
func (m *Metrics) IncCircuitOpen()  { m.CircuitOpens.Add(1) }
func (m *Metrics) IncCacheHit()     { m.CacheHits.Add(1) }
func (m *Metrics) IncCacheMiss()    { m.CacheMisses.Add(1) }

func (m *Metrics) IncStatus(code int) {
	m.statusMu.Lock()
	c, ok := m.StatusCounts[code]
	if !ok {
		c = &atomic.Int64{}
		m.StatusCounts[code] = c
	}
	m.statusMu.Unlock()
	c.Add(1)
}

func (m *Metrics) RecordLatency(d time.Duration) {
	m.totalLatencyMs.Add(d.Milliseconds())
	m.latencyCount.Add(1)

	idx := len(latencyBuckets) // default: >=1s bucket
	for i, bucket := range latencyBuckets {
		if d < bucket {
			idx = i
			break
		}
	}
	m.latencyBuckets[idx].Add(1)
}

// percentileMs estimates a percentile (0.0–1.0) from histogram buckets.
func (m *Metrics) percentileMs(p float64) int64 {
	total := m.latencyCount.Load()
	if total == 0 {
		return 0
	}
	target := int64(math.Ceil(p * float64(total)))
	cumulative := int64(0)

	bucketUpperMs := []int64{10, 50, 100, 500, 1000, math.MaxInt64}
	bucketLowerMs := []int64{0, 10, 50, 100, 500, 1000}

	for i := range bucketUpperMs {
		count := m.latencyBuckets[i].Load()
		cumulative += count
		if cumulative >= target && count > 0 {
			// Linear interpolation within bucket
			prevCum := cumulative - count
			fraction := float64(target-prevCum) / float64(count)
			lo := float64(bucketLowerMs[i])
			hi := float64(bucketUpperMs[i])
			if hi > 5000 {
				// Last bucket has no upper bound; use 2× mean as estimate
				hi = (float64(m.totalLatencyMs.Load()) / float64(total)) * 2
			}
			return int64(lo + fraction*(hi-lo))
		}
	}
	return m.totalLatencyMs.Load() / total
}

type snapshot struct {
	UptimeSeconds  float64            `json:"uptime_seconds"`
	TotalRequests  int64              `json:"total_requests"`
	ActiveRequests int64              `json:"active_requests"`
	ErrorRate      float64            `json:"error_rate"`
	StatusCodes    map[string]int64   `json:"status_codes"`
	LatencyP50Ms   int64              `json:"latency_p50_ms"`
	LatencyP99Ms   int64              `json:"latency_p99_ms"`
	Cache          cacheSnapshot      `json:"cache"`
	RateLimiter    rateLimiterSnapshot `json:"rate_limiter"`
}

type cacheSnapshot struct {
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

type rateLimiterSnapshot struct {
	Rejected int64 `json:"rejected"`
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	total := m.TotalRequests.Load()
	errors := m.TotalErrors.Load()

	errRate := 0.0
	if total > 0 {
		errRate = float64(errors) / float64(total)
	}

	m.statusMu.Lock()
	codes := make(map[string]int64, len(m.StatusCounts))
	for code, cnt := range m.StatusCounts {
		codes[fmt.Sprintf("%d", code)] = cnt.Load()
	}
	m.statusMu.Unlock()

	hits := m.CacheHits.Load()
	misses := m.CacheMisses.Load()
	hitRate := 0.0
	if hits+misses > 0 {
		hitRate = float64(hits) / float64(hits+misses)
	}

	snap := snapshot{
		UptimeSeconds:  time.Since(m.Uptime).Seconds(),
		TotalRequests:  total,
		ActiveRequests: m.ActiveRequests.Load(),
		ErrorRate:      errRate,
		StatusCodes:    codes,
		LatencyP50Ms:   m.percentileMs(0.50),
		LatencyP99Ms:   m.percentileMs(0.99),
		Cache: cacheSnapshot{
			Hits:    hits,
			Misses:  misses,
			HitRate: hitRate,
		},
		RateLimiter: rateLimiterSnapshot{
			Rejected: m.RateLimited.Load(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}
