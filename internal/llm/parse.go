package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// rawDraft mirrors the JSON shape we ask the model to emit. Time is
// parsed as a string so callers can return RFC 3339, RFC 1123, or
// null without surfacing each shape as a parse error.
type rawDraft struct {
	Description string     `json:"description"`
	DueAt       *string    `json:"due_at,omitempty"`
	Confidence  float64    `json:"confidence"`
	Entities    []rawEntity `json:"entities,omitempty"`
}

type rawEntity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// parseDrafts pulls a JSON array of drafts out of the model's reply.
// Models often wrap the array in code fences or add a one-line
// preamble — strip both before decoding. Return [] when the model
// emits an empty array; surface a parse error only when the payload
// is genuinely unparseable.
func parseDrafts(reply string) ([]domain.CommitmentDraft, error) {
	cleaned := stripCodeFences(strings.TrimSpace(reply))
	cleaned = trimToFirstArray(cleaned)
	if cleaned == "" || cleaned == "[]" {
		return nil, nil
	}
	var raws []rawDraft
	if err := json.Unmarshal([]byte(cleaned), &raws); err != nil {
		return nil, fmt.Errorf("llm: parse drafts: %w", err)
	}
	out := make([]domain.CommitmentDraft, 0, len(raws))
	for _, r := range raws {
		desc := strings.TrimSpace(r.Description)
		if desc == "" {
			continue
		}
		d := domain.CommitmentDraft{
			Description: desc,
			Confidence:  clamp01(r.Confidence),
		}
		if r.DueAt != nil && *r.DueAt != "" {
			if t, err := parseFlexibleTime(*r.DueAt); err == nil {
				utc := t.UTC()
				d.DueAt = &utc
			}
		}
		for _, e := range r.Entities {
			kind := strings.TrimSpace(e.Kind)
			name := strings.TrimSpace(e.Name)
			if kind == "" || name == "" {
				continue
			}
			d.Entities = append(d.Entities, domain.EntityRef{Kind: domain.EntityKind(kind), Name: name})
		}
		out = append(out, d)
	}
	return out, nil
}

func stripCodeFences(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove leading fence (with or without language tag) and trailing fence.
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

// trimToFirstArray locates the outermost JSON array in s. Models
// occasionally emit a single object {commitments: [...]} or wrap the
// array in a one-line preamble. We find the first '[' and the
// matching ']' and return that slice. If the brackets don't balance,
// return the slice from the first '[' onward so json.Unmarshal can
// surface the parse error rather than silently treating the input as
// empty.
func trimToFirstArray(s string) string {
	start := strings.IndexByte(s, '[')
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func parseFlexibleTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("llm: cannot parse due_at %q", s)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
