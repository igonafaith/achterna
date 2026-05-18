package circuit

import (
	"sync"
	"time"
)

type State int

const (
	StateClosed   State = iota // normal — all requests forwarded
	StateOpen                  // origin down — return 503 immediately
	StateHalfOpen              // probe a few requests
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failures         int
	successes        int
	totalOpens       int64
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	openedAt         time.Time
	countedCodes     map[int]bool
	probeAllowed     bool
	OnStateChange    func(from, to State)
}

type Config struct {
	FailureThreshold  int
	SuccessThreshold  int
	Timeout           time.Duration
	CountedStatusCodes []int
}

func New(cfg Config) *CircuitBreaker {
	codes := make(map[int]bool, len(cfg.CountedStatusCodes))
	for _, c := range cfg.CountedStatusCodes {
		codes[c] = true
	}
	return &CircuitBreaker{
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		countedCodes:     codes,
	}
}

// Allow returns true if the request should be forwarded to origin.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) > cb.timeout {
			cb.transition(StateHalfOpen)
			return true
		}
		return false
	case StateHalfOpen:
		if cb.probeAllowed {
			cb.probeAllowed = false
			return true
		}
		return false
	}
	return false
}

// RecordResult updates the circuit state based on the response status code.
func (cb *CircuitBreaker) RecordResult(statusCode int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	isFailure := cb.countedCodes[statusCode]

	switch cb.state {
	case StateClosed:
		if isFailure {
			cb.failures++
			if cb.failures >= cb.failureThreshold {
				cb.transition(StateOpen)
			}
		} else {
			cb.failures = 0
		}

	case StateHalfOpen:
		if isFailure {
			cb.successes = 0
			cb.transition(StateOpen)
		} else {
			cb.successes++
			if cb.successes >= cb.successThreshold {
				cb.transition(StateClosed)
			} else {
				cb.probeAllowed = true // allow next probe
			}
		}
	}
}

// Reset forces the circuit to closed state (admin action).
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transition(StateClosed)
	cb.failures = 0
	cb.successes = 0
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// TotalOpens returns how many times the circuit has opened.
func (cb *CircuitBreaker) TotalOpens() int64 {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.totalOpens
}

// ConsecutiveFailures returns current failure count (only meaningful in closed state).
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

// RetryAfter returns the estimated remaining time the circuit will stay open.
// Returns zero if the circuit is not open.
func (cb *CircuitBreaker) RetryAfter() time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state != StateOpen {
		return 0
	}
	remaining := cb.timeout - time.Since(cb.openedAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (cb *CircuitBreaker) transition(to State) {
	from := cb.state
	cb.state = to
	if to == StateOpen {
		cb.openedAt = time.Now()
		cb.totalOpens++
		cb.failures = 0
		cb.successes = 0
	} else if to == StateClosed {
		cb.failures = 0
		cb.successes = 0
	} else if to == StateHalfOpen {
		cb.failures = 0
		cb.successes = 0
		cb.probeAllowed = false // the triggering request is the first probe
	}
	if cb.OnStateChange != nil && from != to {
		go cb.OnStateChange(from, to)
	}
}
