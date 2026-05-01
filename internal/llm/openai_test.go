package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProvider_ExtractCommitments_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth = %q", got)
		}
		var req openaiChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format = %+v", req.ResponseFormat)
		}
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{{Message: openaiMessage{
				Role:    "assistant",
				Content: `{"commitments":[{"description":"ship report","confidence":0.85}]}`,
			}}},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", "gpt-test", WithOpenAIBaseURL(srv.URL), WithOpenAIHTTPClient(srv.Client()))
	drafts, err := p.ExtractCommitments(context.Background(), "extract")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Description != "ship report" {
		t.Errorf("drafts = %+v", drafts)
	}
}

func TestOpenAIProvider_FallsBackToBareArray(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Choices: []openaiChoice{{Message: openaiMessage{
				Content: `[{"description":"x","confidence":0.5}]`,
			}}},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk", "", WithOpenAIBaseURL(srv.URL), WithOpenAIHTTPClient(srv.Client()))
	drafts, err := p.ExtractCommitments(context.Background(), "x")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 1 {
		t.Errorf("drafts = %+v", drafts)
	}
}

func TestOpenAIProvider_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(openaiChatResponse{
			Error: &openaiError{Type: "rate_limit_exceeded", Message: "slow down"},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk", "", WithOpenAIBaseURL(srv.URL), WithOpenAIHTTPClient(srv.Client()))
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "rate_limit_exceeded") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenAIProvider_MissingKey(t *testing.T) {
	t.Parallel()
	p := NewOpenAIProvider("", "", WithOpenAIBaseURL("http://example.invalid"))
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
}
