package llm

import (
	"strings"
	"testing"
)

func TestParseDrafts_BareArray(t *testing.T) {
	t.Parallel()
	in := `[{"description":"call alex","due_at":"2026-05-02T15:00:00Z","confidence":0.9,"entities":[{"kind":"person","name":"Alex"}]}]`
	got, err := parseDrafts(in)
	if err != nil {
		t.Fatalf("parseDrafts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	d := got[0]
	if d.Description != "call alex" {
		t.Errorf("description = %q", d.Description)
	}
	if d.Confidence != 0.9 {
		t.Errorf("confidence = %v", d.Confidence)
	}
	if d.DueAt == nil || d.DueAt.IsZero() {
		t.Errorf("due_at not parsed")
	}
	if len(d.Entities) != 1 || d.Entities[0].Name != "Alex" {
		t.Errorf("entities = %+v", d.Entities)
	}
}

func TestParseDrafts_StripsCodeFence(t *testing.T) {
	t.Parallel()
	in := "```json\n[{\"description\":\"x\",\"confidence\":0.5}]\n```"
	got, err := parseDrafts(in)
	if err != nil {
		t.Fatalf("parseDrafts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestParseDrafts_TrimsPreamble(t *testing.T) {
	t.Parallel()
	in := `Here is the JSON:
[{"description":"x","confidence":0.5}]`
	got, err := parseDrafts(in)
	if err != nil {
		t.Fatalf("parseDrafts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestParseDrafts_EmptyArray(t *testing.T) {
	t.Parallel()
	got, err := parseDrafts("[]")
	if err != nil {
		t.Fatalf("parseDrafts: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseDrafts_ClampsConfidence(t *testing.T) {
	t.Parallel()
	in := `[{"description":"a","confidence":1.5},{"description":"b","confidence":-0.2}]`
	got, err := parseDrafts(in)
	if err != nil {
		t.Fatalf("parseDrafts: %v", err)
	}
	if got[0].Confidence != 1 {
		t.Errorf("got[0] = %v", got[0].Confidence)
	}
	if got[1].Confidence != 0 {
		t.Errorf("got[1] = %v", got[1].Confidence)
	}
}

func TestParseDrafts_DropsEmptyDescription(t *testing.T) {
	t.Parallel()
	in := `[{"description":"","confidence":0.9},{"description":"keep","confidence":0.4}]`
	got, err := parseDrafts(in)
	if err != nil {
		t.Fatalf("parseDrafts: %v", err)
	}
	if len(got) != 1 || got[0].Description != "keep" {
		t.Errorf("got = %+v", got)
	}
}

func TestParseDrafts_BadJSONErrors(t *testing.T) {
	t.Parallel()
	_, err := parseDrafts("[{not json")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "parse drafts") {
		t.Errorf("err = %v", err)
	}
}

func TestParseDrafts_FlexibleTimeFormats(t *testing.T) {
	t.Parallel()
	cases := []string{
		`[{"description":"a","due_at":"2026-05-01","confidence":0.5}]`,
		`[{"description":"a","due_at":"2026-05-01T15:00:00Z","confidence":0.5}]`,
		`[{"description":"a","due_at":"2026-05-01 15:00:00","confidence":0.5}]`,
	}
	for _, in := range cases {
		got, err := parseDrafts(in)
		if err != nil {
			t.Errorf("parseDrafts(%q): %v", in, err)
			continue
		}
		if got[0].DueAt == nil {
			t.Errorf("parseDrafts(%q): due_at nil", in)
		}
	}
}
