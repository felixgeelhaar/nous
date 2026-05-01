package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// scriptedFromMap is a tiny CommitmentExtractor whose output is
// pre-canned per case input. Used to drive Run with deterministic
// data instead of a live LLM.
type scriptedFromMap struct {
	out map[string][]domain.CommitmentDraft
}

func (s *scriptedFromMap) Extract(_ context.Context, in ExtractInput) ([]domain.CommitmentDraft, error) {
	return s.out[in.Text], nil
}

func TestGolden_ScriptedAllPass(t *testing.T) {
	t.Parallel()
	ds := &GoldenDataset{Name: "test", Cases: []GoldenCase{
		{Name: "a", Input: "promise to ship", ExpectedDescriptions: []string{"ship"}},
	}}
	ex := &scriptedFromMap{out: map[string][]domain.CommitmentDraft{
		"promise to ship": {{Description: "ship the build", Confidence: 0.9}},
	}}
	r, err := Run(context.Background(), ex, ds)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.Failed != 0 {
		t.Errorf("failed = %d", r.Failed)
	}
	if r.Recall != 1 {
		t.Errorf("recall = %v", r.Recall)
	}
}

func TestGolden_MissingExpected(t *testing.T) {
	t.Parallel()
	ds := &GoldenDataset{Cases: []GoldenCase{
		{Name: "miss", Input: "x", ExpectedDescriptions: []string{"send report"}},
	}}
	ex := &scriptedFromMap{out: map[string][]domain.CommitmentDraft{"x": nil}}
	r, _ := Run(context.Background(), ex, ds)
	if r.Failed != 1 {
		t.Errorf("failed = %d", r.Failed)
	}
	if len(r.Cases[0].Missing) == 0 {
		t.Error("missing not recorded")
	}
}

func TestGolden_NegativeKeywordTriggersSpurious(t *testing.T) {
	t.Parallel()
	ds := &GoldenDataset{Cases: []GoldenCase{
		{Name: "neg", Input: "x", NegativeKeywords: []string{"forbidden"}},
	}}
	ex := &scriptedFromMap{out: map[string][]domain.CommitmentDraft{
		"x": {{Description: "do the forbidden thing", Confidence: 0.9}},
	}}
	r, _ := Run(context.Background(), ex, ds)
	if r.Failed != 1 {
		t.Errorf("failed = %d", r.Failed)
	}
	if r.Precision != 0 {
		t.Errorf("precision = %v, want 0 (1/1 spurious)", r.Precision)
	}
}

func TestGolden_MinConfidenceGate(t *testing.T) {
	t.Parallel()
	ds := &GoldenDataset{Cases: []GoldenCase{
		{Name: "lowconf", Input: "x", ExpectedDescriptions: []string{"ship"}, MinConfidence: 0.7},
	}}
	ex := &scriptedFromMap{out: map[string][]domain.CommitmentDraft{
		"x": {{Description: "ship the thing", Confidence: 0.5}},
	}}
	r, _ := Run(context.Background(), ex, ds)
	if r.Failed != 1 {
		t.Errorf("failed = %d", r.Failed)
	}
	if len(r.Cases[0].LowConf) == 0 {
		t.Error("low-confidence not recorded")
	}
}

func TestGolden_LoadDataset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	body := `{"name":"unit","cases":[{"name":"x","input":"a","expected_descriptions":["a"]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ds, err := LoadGoldenDataset(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ds.Name != "unit" || len(ds.Cases) != 1 {
		t.Errorf("ds = %+v", ds)
	}
}

func TestGolden_DefaultDataset(t *testing.T) {
	t.Parallel()
	ds := DefaultGoldenDataset()
	if len(ds.Cases) == 0 {
		t.Fatal("default empty")
	}
	for _, c := range ds.Cases {
		if c.Name == "" {
			t.Error("case missing name")
		}
	}
}

func TestGolden_PassRate(t *testing.T) {
	t.Parallel()
	r := Report{Total: 4, Passed: 3}
	if r.PassRate() != 0.75 {
		t.Errorf("pass rate = %v", r.PassRate())
	}
}
