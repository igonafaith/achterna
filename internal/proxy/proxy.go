package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/igonafaith/achterna/internal/config"
)

// hopByHopHeaders are headers that should not be forwarded per RFC 7230.
var hopByHopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"TE", "Trailers", "Transfer-Encoding", "Upgrade",
}

type Proxy struct {
	rp     *httputil.ReverseProxy
	origin *url.URL
	cfg    config.OriginConfig
}

func New(cfg config.OriginConfig) (*Proxy, error) {
	origin, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid origin URL: %w", err)
	}

	p := &Proxy{origin: origin, cfg: cfg}

	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}

	p.rp = &httputil.ReverseProxy{
		Director:       p.director,
		Transport:      transport,
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.errorHandler,
	}

	return p, nil
}

func (p *Proxy) director(req *http.Request) {
	req.URL.Scheme = p.origin.Scheme
	req.URL.Host = p.origin.Host
	req.Host = p.cfg.HostHeader

	// Remove hop-by-hop headers
	for _, h := range hopByHopHeaders {
		req.Header.Del(h)
	}

	// Append client IP to X-Forwarded-For
	clientIP := req.RemoteAddr
	if i := strings.LastIndex(clientIP, ":"); i != -1 {
		clientIP = clientIP[:i]
	}
	clientIP = strings.Trim(clientIP, "[]")
	prior := req.Header.Get("X-Forwarded-For")
	if prior != "" {
		req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	if req.Header.Get("X-Forwarded-Proto") == "" {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Remove hop-by-hop from response
	for _, h := range hopByHopHeaders {
		resp.Header.Del(h)
	}
	return nil
}

func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) || r.Context().Err() == context.DeadlineExceeded {
		http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
		return
	}
	http.Error(w, "Bad Gateway", http.StatusBadGateway)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), p.cfg.Timeout)
	defer cancel()
	p.rp.ServeHTTP(w, r.WithContext(ctx))
}
