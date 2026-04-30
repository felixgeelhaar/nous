package intervention_test

import (
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/risk"
)

func TestEngine_MaybeIntervene(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	c, err := domain.NewCommitment("u1", "follow up with alex", nil, 0.9, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name     string
		score    float64
		conf     float64
		wantNil  bool
		wantType domain.InterventionType
	}{
		{"below threshold => nil", 0.2, 0.9, true, ""},
		{"nudge", 0.6, 0.6, false, domain.InterventionNudge},
		{"escalate", 0.9, 0.6, false, domain.InterventionEscalation},
		{"automation when high confidence", 0.7, 0.96, false, domain.InterventionAutomation},
	}

	eng := intervention.New(intervention.DefaultConfig())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iv, err := eng.MaybeIntervene(c, risk.RiskAssessment{
				Score: tc.score, Confidence: tc.conf,
				Factors: []risk.RiskFactor{{Type: "overdue", Weight: 0.6, Reason: "past due"}},
			}, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if iv != nil {
					t.Errorf("expected nil intervention, got %+v", iv)
				}
				return
			}
			if iv == nil {
				t.Fatalf("expected intervention, got nil")
			}
			if iv.Type != tc.wantType {
				t.Errorf("type = %q, want %q", iv.Type, tc.wantType)
			}
			if iv.CommitmentID == nil || *iv.CommitmentID != c.ID {
				t.Errorf("commitment id not anchored: %+v", iv.CommitmentID)
			}
			if iv.Status != domain.InterventionPending {
				t.Errorf("status = %q, want pending", iv.Status)
			}
		})
	}
}

func TestEngine_MaybeIntervene_NoFactors(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	c, _ := domain.NewCommitment("u1", "follow up", nil, 0.9, now)
	eng := intervention.New(intervention.DefaultConfig())

	// Score above threshold but no factors — should still produce a nudge
	// with a generic message.
	iv, err := eng.MaybeIntervene(c, risk.RiskAssessment{
		Score: 0.6, Confidence: 0.5, Factors: nil,
	}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iv == nil {
		t.Fatal("expected intervention, got nil")
	}
	if iv.Type != domain.InterventionNudge {
		t.Errorf("type = %q, want nudge", iv.Type)
	}
}
