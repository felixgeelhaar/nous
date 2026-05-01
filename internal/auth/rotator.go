package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"
)

// Rotator manages automated JWT signing-key rotation. It owns the
// authoritative KeySet, generates a fresh active key on a schedule,
// and demotes the previous active key into the previous slot for
// `overlap` so live tokens continue to validate while clients refresh.
//
// Operators wire one Rotator and pass it to both the Issuer (for
// signing) and the Verifier (for validation) so all three stay in
// sync.
type Rotator struct {
	mu       sync.RWMutex
	keys     KeySet
	period   time.Duration
	overlap  time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// RotatorConfig tunes the rotator. Period is how often a new active
// key is generated; Overlap is how long the previous key is kept
// alive after rotation. Overlap should be ≥ the longest issued
// token TTL.
type RotatorConfig struct {
	Period  time.Duration
	Overlap time.Duration
}

// NewRotator constructs a rotator seeded with an initial active key.
// Bootstrap secret may be nil; in that case a fresh secret is
// generated. Period and Overlap must both be positive.
func NewRotator(initial []byte, cfg RotatorConfig) (*Rotator, error) {
	if cfg.Period <= 0 || cfg.Overlap <= 0 {
		return nil, errors.New("auth: rotator period and overlap must be positive")
	}
	if cfg.Overlap > cfg.Period {
		return nil, errors.New("auth: overlap must be ≤ period — otherwise multiple previous keys overlap")
	}
	if len(initial) == 0 {
		var err error
		initial, err = generateSigningKey()
		if err != nil {
			return nil, err
		}
	}
	return &Rotator{
		keys:    KeySet{ActiveKID: timeKID(time.Now()), ActiveSecret: initial},
		period:  cfg.Period,
		overlap: cfg.Overlap,
		stopCh:  make(chan struct{}),
	}, nil
}

// Run blocks until ctx is done, rotating the key every Period. Stop
// can be called from another goroutine to unblock Run early.
func (r *Rotator) Run(ctx context.Context) {
	t := time.NewTicker(r.period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-t.C:
			_ = r.RotateNow()
		}
	}
}

// Stop unblocks any goroutine running Run. Safe to call multiple times.
func (r *Rotator) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

// RotateNow generates a new active key, demoting the current active
// to previous. Returns an error only when key generation fails.
func (r *Rotator) RotateNow() error {
	fresh, err := generateSigningKey()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = KeySet{
		ActiveKID:      timeKID(time.Now()),
		ActiveSecret:   fresh,
		PreviousKID:    r.keys.ActiveKID,
		PreviousSecret: r.keys.ActiveSecret,
	}
	return nil
}

// Snapshot returns a copy of the current key set, safe to hand to
// Issuer and Verifier. Both should call this on every signing /
// verification to pick up rotations.
func (r *Rotator) Snapshot() KeySet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := r.keys
	if r.keys.ActiveSecret != nil {
		out.ActiveSecret = append([]byte(nil), r.keys.ActiveSecret...)
	}
	if r.keys.PreviousSecret != nil {
		out.PreviousSecret = append([]byte(nil), r.keys.PreviousSecret...)
	}
	return out
}

func generateSigningKey() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// timeKID derives a stable, monotonic KID from a timestamp so log
// readers can sort tokens by issuance era. Format: "k-<unix>".
func timeKID(t time.Time) string {
	return "k-" + t.UTC().Format("20060102T150405Z")
}
