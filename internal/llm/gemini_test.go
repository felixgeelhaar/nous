package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiProvider_ExtractCommitments_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1beta/models/gemini-test:generateContent") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "key-123" {
			t.Errorf("key = %q", got)
		}
		_ = json.NewEncoder(w).Encode(geminiResponse{
			Candidates: []geminiCandidate{{
				Content: geminiContent{
					Parts: []geminiPart{{Text: `[{"description":"call alex","confidence":0.9}]`}},
				},
			}},
		})
	}))
	defer srv.Close()

	p := NewGeminiProvider("key-123", "gemini-test", WithGeminiBaseURL(srv.URL), WithGeminiHTTPClient(srv.Client()))
	drafts, err := p.ExtractCommitments(context.Background(), "extract")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Description != "call alex" {
		t.Errorf("drafts = %+v", drafts)
	}
}

func TestGeminiProvider_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(geminiResponse{
			Error: &geminiError{Code: 400, Status: "INVALID_ARGUMENT", Message: "bad prompt"},
		})
	}))
	defer srv.Close()

	p := NewGeminiProvider("key", "", WithGeminiBaseURL(srv.URL), WithGeminiHTTPClient(srv.Client()))
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "INVALID_ARGUMENT") {
		t.Errorf("err = %v", err)
	}
}

func TestGeminiProvider_MissingKey(t *testing.T) {
	t.Parallel()
	p := NewGeminiProvider("", "", WithGeminiBaseURL("http://example.invalid"))
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestGeminiProvider_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(geminiResponse{Candidates: nil})
	}))
	defer srv.Close()

	p := NewGeminiProvider("k", "", WithGeminiBaseURL(srv.URL), WithGeminiHTTPClient(srv.Client()))
	drafts, err := p.ExtractCommitments(context.Background(), "x")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 0 {
		t.Errorf("len = %d, want 0", len(drafts))
	}
}
