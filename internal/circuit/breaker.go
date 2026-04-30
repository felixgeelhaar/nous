// Package circuit implements a simple circuit breaker pattern
// for protecting external service calls.
package circuit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed allows requests through.
	StateClosed State = iota
	// StateOpen blocks all requests.
	StateOpen
	// StateHalfOpen allows a single probe request.
	StateHalfOpen
)

// Config holds circuit breaker configuration.
type Config struct {
	// MaxFailures is the number of consecutive failures before opening.
	MaxFailures int
	// Timeout is how long the circuit stays open before trying half-open.
	Timeout time.Duration
	// MaxRequests is the number of requests allowed in half-open state (0 = 1).
	MaxRequests int
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxFailures: 5,
		Timeout:      30 * time.Second,
		MaxRequests: 1,
	}
}

// Breaker implements the circuit breaker pattern.
type Breaker struct {
	config Config
	mu     sync.Mutex

	state     State
	failures  int
	nextTry   time.Time
	halfOpenRequests int
}

// New creates a new circuit breaker with the given config.
func New(config Config) *Breaker {
	if config.MaxFailures == 0 {
		config.MaxFailures = DefaultConfig().MaxFailures
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultConfig().Timeout
	}
	return &Breaker{
		config: config,
		state:  StateClosed,
	}
}

// Call executes fn within the circuit breaker protection.
// If the circuit is open, it returns ErrCircuitOpen.
func (b *Breaker) Call(ctx context.Context, fn func() error) error {
	b.mu.Lock()
	state := b.state
	b.mu.Unlock()

	if state == StateOpen {
		if time.Now().Before(b.nextTry) {
			return ErrCircuitOpen
		}
		b.mu.Lock()
		if b.state == StateOpen {
			b.state = StateHalfOpen
			b.halfOpenRequests = 0
		}
		b.mu.Unlock()
	}

	b.mu.Lock()
	if b.state == StateHalfOpen {
		if b.halfOpenRequests >= b.config.MaxRequests {
			b.mu.Unlock()
			return ErrCircuitOpen
		}
		b.halfOpenRequests++
	}
	b.mu.Unlock()

	err := fn()

	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.onSuccess()
	} else {
		b.onFailure()
	}
	return err
}

// State returns the current circuit breaker state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Failures returns the current consecutive failure count.
func (b *Breaker) Failures() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures
}

func (b *Breaker) onSuccess() {
	b.failures = 0
	if b.state == StateHalfOpen {
		b.state = StateClosed
	}
}

func (b *Breaker) onFailure() {
	b.failures++
	if b.failures >= b.config.MaxFailures {
		b.state = StateOpen
		b.nextTry = time.Now().Add(b.config.Timeout)
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitOpenError returns true if the error is a circuit-open error.
func CircuitOpenError(err error) bool {
	return err == ErrCircuitOpen
}
