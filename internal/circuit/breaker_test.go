package circuit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBreaker_InitialState(t *testing.T) {
	b := New(DefaultConfig())
	if b.State() != StateClosed {
		t.Fatalf("expected closed, got %v", b.State())
	}
	if b.Failures() != 0 {
		t.Fatalf("expected 0 failures, got %d", b.Failures())
	}
}

func TestBreaker_TransitionsToOpen(t *testing.T) {
	cfg := Config{
		MaxFailures: 3,
		Timeout:      100 * time.Millisecond,
	}
	b := New(cfg)

	// Generate failures
	for i := 0; i < 3; i++ {
		err := b.Call(context.Background(), func() error {
			return errors.New("fail")
		})
		if err == nil {
			t.Fatal("expected error")
		}
	}

	if b.State() != StateOpen {
		t.Fatalf("expected open, got %v", b.State())
	}
}

func TestBreaker_BlocksWhenOpen(t *testing.T) {
	cfg := Config{
		MaxFailures: 2,
		Timeout:      50 * time.Millisecond,
	}
	b := New(cfg)

	// Trip the breaker
	for i := 0; i < 2; i++ {
		_ = b.Call(context.Background(), func() error { return errors.New("fail") })
	}

	// Should be blocked
	err := b.Call(context.Background(), func() error { return nil })
	if !CircuitOpenError(err) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_Recovery(t *testing.T) {
	cfg := Config{
		MaxFailures: 2,
		Timeout:      50 * time.Millisecond,
		MaxRequests: 1,
	}
	b := New(cfg)

	// Trip the breaker
	for i := 0; i < 2; i++ {
		_ = b.Call(context.Background(), func() error { return errors.New("fail") })
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Probe with success
	err := b.Call(context.Background(), func() error { return nil })
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if b.State() != StateClosed {
		t.Fatalf("expected closed after success, got %v", b.State())
	}
}

func TestBreaker_HalfOpenThenFail(t *testing.T) {
	cfg := Config{
		MaxFailures: 2,
		Timeout:      50 * time.Millisecond,
		MaxRequests: 1,
	}
	b := New(cfg)

	// Trip the breaker
	for i := 0; i < 2; i++ {
		_ = b.Call(context.Background(), func() error { return errors.New("fail") })
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Probe with failure - should go back to open
	err := b.Call(context.Background(), func() error { return errors.New("fail") })
	if err == nil {
		t.Fatal("expected error")
	}

	if b.State() != StateOpen {
		t.Fatalf("expected open after half-open failure, got %v", b.State())
	}
}

func TestBreaker_CircuitOpenError(t *testing.T) {
	cfg := Config{
		MaxFailures: 1,
		Timeout:      100 * time.Millisecond,
	}
	b := New(cfg)

	// Trip the breaker
	_ = b.Call(context.Background(), func() error { return errors.New("fail") })

	// Should get ErrCircuitOpen
	err := b.Call(context.Background(), func() error { return nil })
	if !CircuitOpenError(err) {
		t.Fatalf("expected CircuitOpenError, got %v", err)
	}
}
