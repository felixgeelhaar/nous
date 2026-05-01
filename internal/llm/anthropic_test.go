package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicProvider_ExtractCommitments_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-test" {
			t.Errorf("api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("version = %q", got)
		}
		var req anthropicMessagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.Model != "claude-test" {
			t.Errorf("model = %q", req.Model)
		}
		_ = json.NewEncoder(w).Encode(anthropicMessagesResponse{
			Content: []anthropicContentBlock{{
				Type: "text",
				Text: `[{"description":"call alex","confidence":0.9}]`,
			}},
		})
	}))
	defer srv.Close()

	p := NewAnthropicProvider("sk-test", "claude-test", WithAnthropicBaseURL(srv.URL), WithAnthropicHTTPClient(srv.Client()))
	drafts, err := p.ExtractCommitments(context.Background(), "extract")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Description != "call alex" {
		t.Errorf("drafts = %+v", drafts)
	}
}

func TestAnthropicProvider_ExtractCommitments_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(anthropicMessagesResponse{
			Error: &anthropicError{Type: "authentication_error", Message: "invalid api-key"},
		})
	}))
	defer srv.Close()

	p := NewAnthropicProvider("bad", "", WithAnthropicBaseURL(srv.URL), WithAnthropicHTTPClient(srv.Client()))
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "authentication_error") {
		t.Errorf("err = %v", err)
	}
}

func TestAnthropicProvider_ExtractCommitments_MissingKey(t *testing.T) {
	t.Parallel()
	p := NewAnthropicProvider("", "", WithAnthropicBaseURL("http://example.invalid"))
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "api key required") {
		t.Errorf("err = %v", err)
	}
}

func TestAnthropicProvider_ExtractCommitments_EmptyContent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(anthropicMessagesResponse{Content: nil})
	}))
	defer srv.Close()

	p := NewAnthropicProvider("sk", "", WithAnthropicBaseURL(srv.URL), WithAnthropicHTTPClient(srv.Client()))
	drafts, err := p.ExtractCommitments(context.Background(), "x")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 0 {
		t.Errorf("len = %d, want 0", len(drafts))
	}
}
