package risk_test

import (
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/google/uuid"
)

func TestEngine_EvaluateCommitment(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	mkCommitment := func(due *time.Time, confidence float64) domain.Commitment {
		c, err := domain.NewCommitment("u1", "task", due, confidence, now)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		return c
	}

	soon := now.Add(30 * time.Minute)
	overdue := now.Add(-15 * time.Minute)
	farFuture := now.Add(72 * time.Hour)

	cases := []struct {
		name      string
		due       *time.Time
		conf      float64
		signals   []domain.ChronosSignal
		minScore  float64
		maxScore  float64
		wantTypes []string
	}{
		{
			name:      "no due, low confidence -> tiny score",
			due:       nil,
			conf:      0.1,
			minScore:  0.0,
			maxScore:  0.1,
			wantTypes: []string{"confidence"},
		},
		{
			name:      "due soon -> due_soon factor present",
			due:       &soon,
			conf:      0.9,
			minScore:  0.45, // 0.3 due_soon + 0.18 confidence
			maxScore:  0.5,
			wantTypes: []string{"due_soon", "confidence"},
		},
		{
			name:      "overdue -> overdue factor present",
			due:       &overdue,
			conf:      0.9,
			minScore:  0.7, // 0.6 overdue + 0.18 confidence
			maxScore:  0.85,
			wantTypes: []string{"overdue", "confidence"},
		},
		{
			name:      "far future -> only confidence",
			due:       &farFuture,
			conf:      0.5,
			minScore:  0.0,
			maxScore:  0.15,
			wantTypes: []string{"confidence"},
		},
		{
			name:     "overdue + signal saturates near 1",
			due:      &overdue,
			conf:     1.0,
			signals:  []domain.ChronosSignal{{ID: uuid.New(), Pattern: "missed_deadline"}},
			minScore: 0.9,
			maxScore: 1.0,
		},
		{
			name:      "no due, zero confidence -> zero score",
			due:       nil,
			conf:      0.0,
			minScore:  0.0,
			maxScore:  0.0,
			wantTypes: nil,
		},
	}

	eng := risk.New(risk.DefaultConfig())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mkCommitment(tc.due, tc.conf)
			got := eng.EvaluateCommitment(c, tc.signals, nil, now)

			if got.Score < tc.minScore-1e-9 || got.Score > tc.maxScore+1e-9 {
				t.Errorf("score = %v, want in [%v, %v]", got.Score, tc.minScore, tc.maxScore)
			}
			if got.Score < 0 || got.Score > 1 {
				t.Errorf("score out of clamped range: %v", got.Score)
			}
			if got.Confidence != tc.conf {
				t.Errorf("confidence passthrough = %v, want %v", got.Confidence, tc.conf)
			}
			for _, want := range tc.wantTypes {
				if !hasFactor(got.Factors, want) {
					t.Errorf("missing factor type %q in %+v", want, got.Factors)
				}
			}
		})
	}
}

func hasFactor(factors []risk.RiskFactor, t string) bool {
	for _, f := range factors {
		if f.Type == t {
			return true
		}
	}
	return false
}
