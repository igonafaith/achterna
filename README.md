# achterna

Lightweight Go reverse proxy for VPS deployments. Sits between Cloudflare Workers and an origin VPS, adding caching, circuit breaking, rate limiting, and observability with a single static binary.

## Architecture

```
Client → Cloudflare Workers → achterna (:8080) → Origin VPS
                                    ↓
                             Admin API (:9090)
```

## Features

- **Reverse proxy** — HTTP/HTTPS forwarding with configurable timeouts and connection pooling
- **In-memory LRU cache** — size-bounded (MB), TTL-based, stale-if-error support
- **Circuit breaker** — 3-state (closed / open / half-open), single-probe recovery
- **Rate limiter** — token bucket with burst, graceful 429 with `Retry-After`
- **Structured logging** — JSON via `log/slog`, request ID on every log line
- **Metrics** — in-process counters, latency histogram, `/metrics` JSON endpoint
- **Health check** — `/healthz` reflects circuit and cache pressure
- **Admin API** — cache purge, circuit reset, live config reload
- **Graceful shutdown** — drains in-flight requests before exit
- **Zero external runtime deps** — single statically-compiled binary

## Requirements

- Go 1.22+
- Linux VPS (for deployment)

## Build

```bash
make build          # produces bin/achterna
make test           # run tests with race detector
make lint           # golangci-lint (must be installed separately)
```

Cross-compile for Linux from Windows or macOS:

```bash
GOOS=linux GOARCH=amd64 go build -o bin/achterna ./cmd/achterna
```

## Configuration

Copy `config.yaml` and edit for your environment:

```yaml
server:
  addr: ":8080"
  read_timeout: 30s
  write_timeout: 60s
  idle_timeout: 120s
  shutdown_timeout: 15s

admin:
  addr: "127.0.0.1:9090"  # bind to localhost only — do not expose publicly

origin:
  url: "https://origin.example.com"      # must include scheme
  host_header: "origin.example.com"      # Host header forwarded to origin
  timeout: 15s
  max_idle_conns: 100
  idle_conn_timeout: 90s

cache:
  enabled: true
  max_size_mb: 256
  default_ttl: 5m
  cacheable_extensions: [.css, .js, .png, .jpg, .jpeg, .gif, .svg, .woff, .woff2, .ico]
  bypass_methods: [POST, PUT, PATCH, DELETE]

circuit_breaker:
  failure_threshold: 5     # consecutive failures before opening
  success_threshold: 3     # consecutive successes to close from half-open
  timeout: 30s             # how long to wait before probing
  counted_status_codes: [502, 503, 504]

rate_limit:
  requests_per_second: 200
  burst: 50

logging:
  level: "info"            # debug | info | warn | error
  format: "json"           # json | text
```

> **Note:** `origin.url` must include the scheme (`https://`). A bare hostname like `origin.example.com` is rejected at startup with a descriptive error.

## Deployment

### 1. Build and copy binary

```bash
GOOS=linux GOARCH=amd64 go build -o bin/achterna ./cmd/achterna
scp bin/achterna user@vps:/opt/achterna/achterna
scp config.yaml user@vps:/opt/achterna/config.yaml
```

### 2. Create a dedicated system user

```bash
sudo useradd -r -s /sbin/nologin achterna
sudo chown -R achterna:achterna /opt/achterna
sudo chmod 750 /opt/achterna
```

### 3. Install and start the systemd service

```bash
sudo cp achterna.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now achterna
sudo systemctl status achterna
```

Or use the Makefile target (requires passwordless sudo on the remote host):

```bash
make deploy
```

## Admin API

All endpoints are served on `127.0.0.1:9090` (localhost only). The default `config.yaml` already binds to `127.0.0.1`; ensure your firewall blocks external access to this port.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health status — 200 healthy, 503 degraded |
| `GET` | `/metrics` | JSON metrics: requests, errors, cache, latency |
| `GET` | `/cache/stats` | Cache size, hit/miss counts |
| `POST` | `/cache/purge?prefix=` | Purge entries by URL prefix; omit `prefix` to purge all |
| `GET` | `/circuit/state` | Circuit state, failure counts, total opens |
| `POST` | `/circuit/reset` | Force circuit to closed state |
| `POST` | `/config/reload` | Hot-reload `config.yaml` without restart |

### Examples

```bash
# Health check
curl http://localhost:9090/healthz

# View metrics
curl http://localhost:9090/metrics | python3 -m json.tool

# Purge all cache
curl -X POST http://localhost:9090/cache/purge

# Purge by prefix
curl -X POST "http://localhost:9090/cache/purge?prefix=/static/"

# Reset circuit breaker
curl -X POST http://localhost:9090/circuit/reset

# Live config reload
curl -X POST http://localhost:9090/config/reload
```

## Response Headers

| Header | Values | Description |
|--------|--------|-------------|
| `X-Request-Id` | UUID | Unique ID for every request |
| `X-Cache` | `HIT` / `MISS` / `BYPASS` / `STALE` | Cache result |
| `X-Cache-Age` | seconds | Age of a cache HIT |
| `X-Circuit` | `closed` / `open` / `half-open` | Circuit state at request time |
| `Retry-After` | seconds | Present on 503 (circuit open) and 429 (rate limited) |

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build binary to `bin/achterna` |
| `make test` | Run tests with race detector |
| `make test-v` | Verbose test output |
| `make lint` | Run golangci-lint |
| `make run` | Build and run locally with `config.yaml` |
| `make clean` | Remove `bin/` |
| `make deploy` | Build and install to `/opt/achterna` (requires sudo) |
| `make reload` | POST `/config/reload` to running instance |
| `make circuit-state` | Show current circuit breaker state |
| `make metrics` | Show metrics JSON |
| `make health` | Show health status |

## License

MIT
