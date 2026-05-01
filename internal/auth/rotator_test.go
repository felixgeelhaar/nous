package auth

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestRotator_BootstrapsWithInitialKey(t *testing.T) {
	t.Parallel()
	seed := []byte("0123456789abcdef0123456789abcdef")
	r, err := NewRotator(seed, RotatorConfig{Period: time.Hour, Overlap: time.Hour})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	snap := r.Snapshot()
	if !bytes.Equal(snap.ActiveSecret, seed) {
		t.Errorf("active secret not bootstrapped from initial")
	}
	if snap.PreviousSecret != nil {
		t.Errorf("previous should be nil at start")
	}
	if snap.ActiveKID == "" {
		t.Errorf("active KID empty")
	}
}

func TestRotator_RotateDemotesActiveToPrevious(t *testing.T) {
	t.Parallel()
	seed := []byte("0123456789abcdef0123456789abcdef")
	r, _ := NewRotator(seed, RotatorConfig{Period: time.Hour, Overlap: time.Hour})
	preKID := r.Snapshot().ActiveKID
	if err := r.RotateNow(); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	snap := r.Snapshot()
	if bytes.Equal(snap.ActiveSecret, seed) {
		t.Errorf("active still equals seed after rotation")
	}
	if !bytes.Equal(snap.PreviousSecret, seed) {
		t.Errorf("previous should equal seed after demotion")
	}
	if snap.PreviousKID != preKID {
		t.Errorf("previous KID = %q, want %q", snap.PreviousKID, preKID)
	}
}

func TestRotator_DualKeyValidatesPreRotationToken(t *testing.T) {
	t.Parallel()
	seed := []byte("0123456789abcdef0123456789abcdef")
	r, _ := NewRotator(seed, RotatorConfig{Period: time.Hour, Overlap: time.Hour})

	// Issue under initial key.
	preSnap := r.Snapshot()
	tok, err := NewIssuer(preSnap, time.Hour).Issue("alice", nil, 0)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// Rotate: previous key now holds the seed under which the token was signed.
	_ = r.RotateNow()
	postSnap := r.Snapshot()

	// Verifier wired with the rotated set must still accept the old token.
	if _, err := NewVerifier(postSnap).Verify(tok); err != nil {
		t.Errorf("pre-rotation token rejected after rotation: %v", err)
	}
}

func TestRotator_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	if _, err := NewRotator(nil, RotatorConfig{}); err == nil {
		t.Error("zero config should error")
	}
	if _, err := NewRotator(nil, RotatorConfig{Period: time.Hour, Overlap: 2 * time.Hour}); err == nil {
		t.Error("overlap > period should error")
	}
}

func TestRotator_RunStopsOnStop(t *testing.T) {
	t.Parallel()
	r, _ := NewRotator(nil, RotatorConfig{Period: time.Minute, Overlap: time.Minute})
	done := make(chan struct{})
	go func() {
		r.Run(context.Background())
		close(done)
	}()
	r.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Run did not return after Stop")
	}
}

func TestRotator_RunStopsOnContext(t *testing.T) {
	t.Parallel()
	r, _ := NewRotator(nil, RotatorConfig{Period: time.Minute, Overlap: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Run did not return after context cancel")
	}
}
