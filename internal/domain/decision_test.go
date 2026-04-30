package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewDecision_Validates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		subject    string
		reason     string
		confidence float64
		wantErr    bool
	}{
		{"happy path", "intervene-on-commitment", "risk score over threshold", 0.9, false},
		{"no subject", "", "reason", 0.5, true},
		{"no reason", "subject", "", 0.5, true},
		{"negative confidence", "subject", "reason", -0.1, true},
		{"over-one confidence", "subject", "reason", 1.1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDecision(tc.subject, tc.reason, tc.confidence, now)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", d)
				}
				if !errors.Is(err, ErrValidation) {
					t.Errorf("err = %v, want wrapping ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !d.CreatedAt.Equal(now) {
				t.Errorf("created_at = %v, want %v", d.CreatedAt, now)
			}
		})
	}
}

func TestDecision_Validate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		d    Decision
		want bool
	}{
		{"valid", Decision{ID: uuid.New(), Subject: "subj", Reason: "reason", Confidence: 0.5, CreatedAt: now}, false},
		{"missing id", Decision{Subject: "subj", Reason: "reason", Confidence: 0.5, CreatedAt: now}, true},
		{"missing subject", Decision{ID: uuid.New(), Reason: "reason", Confidence: 0.5, CreatedAt: now}, true},
		{"missing reason", Decision{ID: uuid.New(), Subject: "subj", Confidence: 0.5, CreatedAt: now}, true},
		{"bad confidence", Decision{ID: uuid.New(), Subject: "subj", Reason: "reason", Confidence: 1.5, CreatedAt: now}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if tc.want {
				if !errors.Is(err, ErrValidation) {
					t.Errorf("expected ErrValidation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
