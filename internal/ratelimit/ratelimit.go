package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	tokens chan struct{}
	done   chan struct{}
	once   sync.Once
}

func New(rps int, burst int) *Limiter {
	l := &Limiter{
		tokens: make(chan struct{}, burst),
		done:   make(chan struct{}),
	}

	// Pre-fill burst capacity
	for i := 0; i < burst; i++ {
		l.tokens <- struct{}{}
	}

	// Refill goroutine — one token per (1s/rps) interval
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(rps))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case l.tokens <- struct{}{}:
				default: // bucket full, discard
				}
			case <-l.done:
				return
			}
		}
	}()

	return l
}

// Allow returns true if the request is within rate limit.
func (l *Limiter) Allow() bool {
	select {
	case <-l.tokens:
		return true
	default:
		return false
	}
}

// Close stops the background refill goroutine. Safe to call multiple times.
func (l *Limiter) Close() {
	l.once.Do(func() { close(l.done) })
}
