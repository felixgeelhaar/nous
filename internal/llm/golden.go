package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GoldenCase is one input/output pair in a regression dataset.
// Input is the raw text the extractor sees; ExpectedDescriptions is
// the set of commitment descriptions the prompt must surface (case
// folded, whitespace-normalised). NegativeKeywords flag false-positive
// descriptions: the extractor must not produce any draft whose
// description contains any of these substrings.
type GoldenCase struct {
	Name                 string   `json:"name"`
	Input                string   `json:"input"`
	OwnerID              string   `json:"owner_id,omitempty"`
	ExpectedDescriptions []string `json:"expected_descriptions"`
	NegativeKeywords     []string `json:"negative_keywords,omitempty"`
	MinConfidence        float64  `json:"min_confidence,omitempty"`
}

// GoldenDataset is a labelled regression set.
type GoldenDataset struct {
	Name  string       `json:"name"`
	Cases []GoldenCase `json:"cases"`
}

// LoadGoldenDataset reads a JSON dataset from path. Use
// [DefaultGoldenDataset] for the canonical fixture shipped in-tree.
func LoadGoldenDataset(path string) (*GoldenDataset, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("golden: read %s: %w", path, err)
	}
	var ds GoldenDataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("golden: parse %s: %w", path, err)
	}
	return &ds, nil
}

// CaseResult is the per-case outcome from a regression run.
type CaseResult struct {
	Name      string
	Pass      bool
	Missing   []string // expected descriptions the extractor failed to surface
	Spurious  []string // descriptions the extractor produced but we flagged as negative
	LowConf   []string // descriptions surfaced but below MinConfidence
	Drafts    int      // total drafts the extractor returned
}

// Report summarises a regression run: per-case results plus an
// aggregate precision / recall / pass-rate.
type Report struct {
	Dataset   string       `json:"dataset"`
	Total     int          `json:"total"`
	Passed    int          `json:"passed"`
	Failed    int          `json:"failed"`
	Precision float64      `json:"precision"`
	Recall    float64      `json:"recall"`
	Cases     []CaseResult `json:"cases"`
}

// Pass reports whether every case passed. The report itself is
// emitted regardless so failed runs can show what slipped.
func (r Report) PassRate() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Passed) / float64(r.Total)
}

// Run executes every case in the dataset against the extractor and
// returns the aggregated report. The extractor parameter is the
// `CommitmentExtractor` port — anything from a ScriptedExtractor to
// a live LLM provider satisfies it.
//
// The harness deliberately uses substring-match (case-folded) for
// expected-description verification so prompts can rephrase as long
// as the key noun is present. Strict equality would over-fit.
func Run(ctx context.Context, ex CommitmentExtractor, ds *GoldenDataset) (Report, error) {
	if ds == nil {
		return Report{}, errors.New("golden: nil dataset")
	}
	r := Report{Dataset: ds.Name, Total: len(ds.Cases)}
	var matched, totalExpected, totalDrafts, spuriousDrafts int

	for _, c := range ds.Cases {
		drafts, err := ex.Extract(ctx, ExtractInput{OwnerID: c.OwnerID, Text: c.Input})
		if err != nil {
			return Report{}, fmt.Errorf("golden: %s: %w", c.Name, err)
		}
		res := CaseResult{Name: c.Name, Pass: true, Drafts: len(drafts)}
		totalDrafts += len(drafts)

		descs := make([]string, 0, len(drafts))
		descConfidence := map[string]float64{}
		for _, d := range drafts {
			n := normaliseDesc(d.Description)
			descs = append(descs, n)
			if conf, seen := descConfidence[n]; !seen || d.Confidence > conf {
				descConfidence[n] = d.Confidence
			}
		}
		sort.Strings(descs)

		// Recall side: every expected description must appear.
		for _, want := range c.ExpectedDescriptions {
			totalExpected++
			needle := normaliseDesc(want)
			hit := false
			for _, got := range descs {
				if strings.Contains(got, needle) {
					if c.MinConfidence > 0 && descConfidence[got] < c.MinConfidence {
						res.LowConf = append(res.LowConf, want)
						res.Pass = false
						hit = true // count toward "found", just below confidence floor
						break
					}
					hit = true
					matched++
					break
				}
			}
			if !hit {
				res.Missing = append(res.Missing, want)
				res.Pass = false
			}
		}

		// Precision side: descriptions that include any negative keyword
		// are spurious.
		for _, got := range descs {
			for _, neg := range c.NegativeKeywords {
				if strings.Contains(got, normaliseDesc(neg)) {
					res.Spurious = append(res.Spurious, got)
					res.Pass = false
					spuriousDrafts++
				}
			}
		}

		if res.Pass {
			r.Passed++
		} else {
			r.Failed++
		}
		r.Cases = append(r.Cases, res)
	}

	if totalExpected > 0 {
		r.Recall = float64(matched) / float64(totalExpected)
	}
	if totalDrafts > 0 {
		r.Precision = 1 - float64(spuriousDrafts)/float64(totalDrafts)
	}
	return r, nil
}

// DefaultGoldenDataset returns the canonical commitment-extraction
// regression set shipped with Nous. Operators add their own cases by
// pointing [LoadGoldenDataset] at their own JSON file.
func DefaultGoldenDataset() *GoldenDataset {
	return &GoldenDataset{
		Name: "nous-default-v1",
		Cases: []GoldenCase{
			{
				Name:                 "explicit-promise",
				Input:                "I'll send the report by Friday.",
				ExpectedDescriptions: []string{"send the report"},
			},
			{
				Name:                 "with-deadline",
				Input:                "Need to deploy the migration before Tuesday's launch.",
				ExpectedDescriptions: []string{"deploy", "migration"},
			},
			{
				Name:                 "rejects-thinking-out-loud",
				Input:                "Just thinking out loud, maybe we'd consider rolling that out next quarter.",
				ExpectedDescriptions: []string{},
				NegativeKeywords:     []string{"roll", "consider"},
			},
			{
				Name:                 "two-commitments",
				Input:                "I'll review the PR tonight and send the summary tomorrow morning.",
				ExpectedDescriptions: []string{"review the pr", "send the summary"},
			},
			{
				Name:                 "negation-aware",
				Input:                "We won't be able to ship this week.",
				ExpectedDescriptions: []string{},
				NegativeKeywords:     []string{"ship"},
			},
		},
	}
}

func normaliseDesc(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Collapse internal whitespace.
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}
