package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"
)

// NewMockOrigin creates a test HTTP server simulating an origin.
func NewMockOrigin() *httptest.Server {
	mux := http.NewServeMux()

	// Static assets — cacheable
	mux.HandleFunc("/assets/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte("body { color: red; }"))
	})

	mux.HandleFunc("/assets/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-bytes"))
	})

	// Dynamic endpoint — not cached
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ts": time.Now().UnixMilli()})
	})

	// Simulated failure
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	// Simulated slow response
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write([]byte("slow"))
	})

	// Normal page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	return httptest.NewServer(mux)
}

// MockTransport allows injecting a fake origin into the proxy transport.
type MockTransport struct {
	Handler http.Handler
}

func (t *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.Handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}
