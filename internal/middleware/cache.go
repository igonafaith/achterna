package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/igonafaith/achterna/internal/cache"
	"github.com/igonafaith/achterna/internal/config"
)

// cacheResponseRecorder captures the response so it can be stored in cache.
type cacheResponseRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func newCacheRecorder(w http.ResponseWriter) *cacheResponseRecorder {
	return &cacheResponseRecorder{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

func (cr *cacheResponseRecorder) WriteHeader(code int) {
	cr.status = code
	cr.ResponseWriter.WriteHeader(code)
}

func (cr *cacheResponseRecorder) Write(b []byte) (int, error) {
	cr.body.Write(b)
	return cr.ResponseWriter.Write(b)
}

func (cr *cacheResponseRecorder) Header() http.Header {
	return cr.ResponseWriter.Header()
}

// CacheCheck serves from cache on HIT; on MISS passes through and stores
// cacheable responses.
func CacheCheck(c cache.Cache, cfg config.CacheConfig) Middleware {
	bypassMethods := make(map[string]bool, len(cfg.BypassMethods))
	for _, m := range cfg.BypassMethods {
		bypassMethods[strings.ToUpper(m)] = true
	}

	cacheableExts := make(map[string]bool, len(cfg.CacheableExtensions))
	for _, ext := range cfg.CacheableExtensions {
		cacheableExts[strings.ToLower(ext)] = true
	}

	cacheableStatuses := map[int]bool{200: true, 301: true, 304: true}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				w.Header().Set("X-Cache", "BYPASS")
				next.ServeHTTP(w, r)
				return
			}

			// Bypass non-GET/HEAD and non-cacheable methods
			if bypassMethods[r.Method] || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
				w.Header().Set("X-Cache", "BYPASS")
				next.ServeHTTP(w, r)
				return
			}

			// Bypass if path extension not cacheable
			ext := strings.ToLower(filepath.Ext(r.URL.Path))
			if ext == "" || !cacheableExts[ext] {
				w.Header().Set("X-Cache", "BYPASS")
				next.ServeHTTP(w, r)
				return
			}

			key := cache.DeriveKey(r)

			// Check cache
			if entry, ok := c.Get(key); ok {
				age := int(time.Since(entry.ExpiresAt.Add(-cfg.DefaultTTL)).Seconds())
				if age < 0 {
					age = 0
				}
				copyHeader(w.Header(), entry.Header)
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("X-Cache-Age", fmt.Sprintf("%d", age))
				w.WriteHeader(entry.StatusCode)
				w.Write(entry.Body) //nolint:errcheck
				return
			}

			// MISS — record response and potentially cache it
			rec := newCacheRecorder(w)
			w.Header().Set("X-Cache", "MISS")
			next.ServeHTTP(rec, r)

			// Decide whether to cache
			if !cacheableStatuses[rec.status] {
				return
			}
			cc := rec.Header().Get("Cache-Control")
			if strings.Contains(cc, "no-store") || strings.Contains(cc, "private") {
				return
			}
			if rec.Header().Get("Set-Cookie") != "" {
				return
			}

			h := rec.Header().Clone()
			for _, k := range []string{"X-Cache", "X-Cache-Age", "X-Circuit", "X-Request-Id"} {
				h.Del(k)
			}
			entry := &cache.Entry{
				StatusCode: rec.status,
				Header:     h,
				Body:       rec.body.Bytes(),
				ExpiresAt:  time.Now().Add(cfg.DefaultTTL),
			}
			c.Set(key, entry)
		})
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
