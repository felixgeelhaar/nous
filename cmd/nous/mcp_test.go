package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("NOUS_MCP_TEST_KEY", "")
	if got := envOr("NOUS_MCP_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("unset env: got %q want fallback", got)
	}
	t.Setenv("NOUS_MCP_TEST_KEY", "explicit")
	if got := envOr("NOUS_MCP_TEST_KEY", "fallback"); got != "explicit" {
		t.Errorf("set env: got %q want explicit", got)
	}
}

func TestMcpHTTP_DecodesSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "n": 7})
	}))
	defer srv.Close()
	hc := &http.Client{Timeout: 2 * time.Second}
	var out struct {
		OK bool `json:"ok"`
		N  int  `json:"n"`
	}
	if err := mcpHTTP(context.Background(), hc, http.MethodGet, srv.URL+"/x", "secret", nil, &out); err != nil {
		t.Fatalf("mcpHTTP: %v", err)
	}
	if !out.OK || out.N != 7 {
		t.Errorf("decoded out = %+v", out)
	}
}

func TestMcpHTTP_PropagatesNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	err := mcpHTTP(context.Background(), &http.Client{Timeout: time.Second}, http.MethodGet, srv.URL+"/x", "", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("err = %v", err)
	}
}

func TestMcpHTTP_NilOutSkipsDecode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not even json`))
	}))
	defer srv.Close()
	if err := mcpHTTP(context.Background(), &http.Client{Timeout: time.Second}, http.MethodGet, srv.URL+"/x", "", nil, nil); err != nil {
		t.Errorf("nil out should swallow body: %v", err)
	}
}

func TestMcpExtractInputJSON_RoundTrip(t *testing.T) {
	t.Parallel()
	in := mcpExtractInput{
		OwnerID: "alice",
		Text:    "hi",
		Hints:   []string{"a"},
		SourceRefs: []mcpExtractSource{
			{Kind: "git", Locator: "abc"},
		},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got mcpExtractInput
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OwnerID != "alice" || got.Text != "hi" || len(got.SourceRefs) != 1 {
		t.Errorf("round-trip = %+v", got)
	}
}
