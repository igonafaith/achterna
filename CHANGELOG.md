# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added
- `POST /config/reload` admin endpoint is now functional. It re-reads `config.yaml`, validates it, and applies hot-reloadable changes (currently: `logging.level`). Other fields (origin URL, timeouts, cache size) require a process restart. Previously the endpoint was registered only when a non-nil reloader was passed — main.go was passing `nil`, making the route silently absent and returning 404.
- `config.NewReloader(path, *slog.LevelVar)` — new type in the config package that implements the `configReloader` interface required by the admin server.
- Logger now uses `*slog.LevelVar` instead of a static `slog.Level`, enabling the log level to be changed atomically at runtime without restart.

### Fixed
- Proxy-specific response headers (`X-Cache`, `X-Cache-Age`, `X-Circuit`, `X-Request-Id`) are now stripped before entries are stored in cache, preventing stale responses from carrying headers that belong to the request that originally populated the cache.
- Circuit breaker half-open state now allows only **one probe request at a time**. Previously all concurrent requests were forwarded during half-open, risking overload on a recovering origin. Additional requests return 503/stale until the probe result is recorded.
- `origin.url` without a scheme (e.g. bare `origin.example.com`) is now rejected at startup with a descriptive error instead of silently forwarding requests with an empty `Host` header.
- Expired cache entries are no longer evicted on `Get()` miss — they are left in place for `GetStale()` to serve during circuit-open stale-if-error recovery. Eviction now happens only under LRU size pressure in `Set()`.
- Admin port default changed from `:9090` (all interfaces) to `127.0.0.1:9090` (localhost only) in `config.yaml`. Previously any host with the default config would expose cache purge and circuit reset to the public internet.
- Project renamed from `rproxy` → `achterna`. Module path updated to `github.com/igonafaith/achterna`, binary, service file, deploy path, and systemd unit updated consistently.

### Changed
- `TestStaleCache` integration test now includes `CacheCheck` in the middleware chain to exercise the full request stack and catch proxy-header leakage in cached entries.

---

## [0.1.0] - 2026-05-16

Initial implementation across four phases.

### Added

**Core proxy**
- `net/http/httputil.ReverseProxy` wrapper with configurable director (URL rewrite, Host header) and error handler returning 502/504.
- YAML config loader with strict field validation (`KnownFields`), defaults, and `origin.url` scheme check.
- `go 1.22` method-pattern routing for admin endpoints.

**Reliability layer**
- In-memory LRU cache bounded by MB, TTL-based expiry, `GetStale()` for stale-if-error, `DeleteByPrefix` for selective or full purge.
- Cache key derivation: `METHOD:host:path?sorted_query` — normalised and collision-resistant.
- Cache middleware: serves HIT from cache, records MISS response body, skips non-GET/HEAD and non-cacheable extensions, respects `no-store` / `private` / `Set-Cookie`.
- 3-state circuit breaker (closed / open / half-open) with configurable failure threshold, success threshold, and open timeout. `OnStateChange` callback for state-change logging.
- Circuit breaker middleware with stale-if-error fallback via `GetStale()` and `Retry-After` header on 503.
- Token bucket rate limiter with burst support. `sync.Once`-protected `Close()` prevents double-close panic.
- Rate limit middleware returning 429 with `Retry-After: 1`.

**Observability**
- Request ID middleware using `crypto/rand`-generated UUIDs, propagated via context key.
- Structured access logging via `log/slog` (JSON or text) with request ID, method, path, status, latency.
- Panic recovery middleware → 500.
- Metrics middleware: tracks total requests, active connections, errors, status code breakdown, latency histogram (6 buckets: <10ms, <50ms, <100ms, <500ms, <1s, ≥1s).
- In-process `/metrics` JSON endpoint with request counts, cache stats, circuit stats, and p50/p95/p99 latency estimates.
- `/healthz` endpoint: 200 healthy, 503 degraded (circuit open or cache >90% full).
- Admin API on separate port `:9090`: health, metrics, cache stats, cache purge, circuit state, circuit reset, hot config reload.

**Hardening and ops**
- Graceful shutdown with configurable drain timeout; explicit `ratelimit.Close()` on shutdown.
- `achterna.service` systemd unit with security directives: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `LimitNOFILE=65536`.
- Makefile with build, test (race-enabled), lint, deploy, and operational shortcut targets.
- 7 integration tests: `TestNormalProxy`, `TestCacheHit`, `TestCacheBypassPOST`, `TestCircuitTrip`, `TestCircuitRecovery`, `TestRateLimit`, `TestStaleCache`.

### Fixed (during initial development)
- Middleware chain order corrected: `requestIDMW` (outermost) → `Recovery` → `Logger` → `Metrics` → `RateLimit` → `CacheCheck` → `CircuitBreak` (innermost).
- `X-Cache-Age` header was emitting an empty string — fixed to `fmt.Sprintf("%d", age)`.
- `X-Circuit` header was set after `WriteHeader` (no-op) — moved before calling next handler.
- `Retry-After` on circuit-open responses now uses `cb.RetryAfter().Seconds()` instead of a hardcoded value.
- `HostHeader` config default now correctly extracts `url.Host` from the origin URL rather than the full URL string.
- Status code keys in metrics map use `"200"` format instead of `"OK"`.
- Integer division in latency percentile calculation replaced with `float64` arithmetic.
- `errorHandler` uses `errors.Is(err, context.DeadlineExceeded)` for reliable timeout detection.
- Admin `cachePurge` uses the `cache.Cache` interface method directly — removed type assertion to `*cache.MemoryCache`.
