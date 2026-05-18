package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/igonafaith/achterna/internal/admin"
	"github.com/igonafaith/achterna/internal/cache"
	"github.com/igonafaith/achterna/internal/circuit"
	"github.com/igonafaith/achterna/internal/config"
	"github.com/igonafaith/achterna/internal/ctxkey"
	"github.com/igonafaith/achterna/internal/metrics"
	"github.com/igonafaith/achterna/internal/middleware"
	"github.com/igonafaith/achterna/internal/proxy"
	"github.com/igonafaith/achterna/internal/ratelimit"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	// 1. Load config
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. Init logger with LevelVar for hot-reload
	lv := &slog.LevelVar{}
	logger := buildLogger(cfg.Logging, lv)

	// 3. Init components
	c := cache.New(cfg.Cache.MaxSizeMB)

	cb := circuit.New(circuit.Config{
		FailureThreshold:   cfg.CircuitBreaker.FailureThreshold,
		SuccessThreshold:   cfg.CircuitBreaker.SuccessThreshold,
		Timeout:            cfg.CircuitBreaker.Timeout,
		CountedStatusCodes: cfg.CircuitBreaker.CountedStatusCodes,
	})
	cb.OnStateChange = func(from, to circuit.State) {
		logger.Warn("circuit breaker state changed",
			slog.String("from", from.String()),
			slog.String("to", to.String()),
		)
	}

	rl := ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	// rl.Close() is called explicitly in graceful shutdown below; no defer here.

	m := metrics.New()

	p, err := proxy.New(cfg.Origin)
	if err != nil {
		logger.Error("failed to init proxy", slog.Any("error", err))
		os.Exit(1)
	}

	// 4. Build middleware chain
	// Execution order outermost → innermost:
	//   requestID → Recovery → Logger → Metrics → RateLimit → CacheCheck → CircuitBreak → Proxy
	requestIDMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ctxkey.WithRequestID(r.Context())
			rid := ctxkey.RequestID(ctx)
			w.Header().Set("X-Request-Id", rid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	handler := middleware.Chain(
		p,
		middleware.CircuitBreak(cb, c, m),   // innermost — closest to proxy
		middleware.CacheCheck(c, cfg.Cache),  // cache check before circuit
		middleware.RateLimit(rl, m),          // throttle before cache
		middleware.Metrics(m),               // track all requests
		middleware.Logger(logger),           // log with request ID in context
		middleware.Recovery(logger),         // catch panics from all inner layers
		requestIDMW,                         // outermost — sets request ID first
	)

	// 5. Proxy server
	proxySrv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// 6. Admin server
	reloader := config.NewReloader(*cfgPath, lv)
	adminHandler := admin.New(cb, c, m, logger, reloader)
	adminSrv := &http.Server{
		Addr:         cfg.Admin.Addr,
		Handler:      adminHandler.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// 7. Start servers
	go func() {
		logger.Info("proxy server starting", slog.String("addr", cfg.Server.Addr))
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("proxy server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	go func() {
		logger.Info("admin server starting", slog.String("addr", cfg.Admin.Addr))
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("admin server error", slog.Any("error", err))
		}
	}()

	// 8. Wait for signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	// 9. Graceful shutdown
	logger.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := proxySrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("proxy shutdown error", slog.Any("error", err))
	}
	if err := adminSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("admin shutdown error", slog.Any("error", err))
	}

	rl.Close()
	logger.Info("shutdown complete")
}

func buildLogger(cfg config.LoggingConfig, lv *slog.LevelVar) *slog.Logger {
	lv.Set(config.LevelFromString(cfg.Level))
	opts := &slog.HandlerOptions{Level: lv}
	if cfg.Format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
