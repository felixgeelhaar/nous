package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerRateLimitMiddleware_KeysByQueryParam(t *testing.T) {
	t.Parallel()
	limiter := NewRateLimiter(100, 1)
	called := 0
	h := OwnerRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }), limiter)

	for _, owner := range []string{"alice", "alice"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/commitments?owner_id="+owner, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		_ = rr
	}
	if called != 1 {
		t.Errorf("called = %d, want 1 (second request should be limited)", called)
	}

	// Different owner gets its own bucket.
	req := httptest.NewRequest(http.MethodGet, "/v1/commitments?owner_id=bob", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if called != 2 {
		t.Errorf("called = %d, want 2 (bob has separate bucket)", called)
	}
}

func TestOwnerRateLimitMiddleware_KeysByHeader(t *testing.T) {
	t.Parallel()
	limiter := NewRateLimiter(100, 1)
	called := 0
	h := OwnerRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }), limiter)

	req := httptest.NewRequest(http.MethodGet, "/v1/decisions?subject=x", nil)
	req.Header.Set("X-Nous-Owner", "carol")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if called != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

func TestOwnerRateLimitMiddleware_KeysByBody(t *testing.T) {
	t.Parallel()
	limiter := NewRateLimiter(100, 1)
	called := 0
	body := ""
	h := OwnerRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		called++
	}), limiter)

	req := httptest.NewRequest(http.MethodPost, "/v1/extract", strings.NewReader(`{"owner_id":"dave","text":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if called != 1 {
		t.Errorf("called = %d, want 1", called)
	}
	// Body must remain readable by the inner handler.
	if !strings.Contains(body, `"owner_id":"dave"`) {
		t.Errorf("body not restored: %q", body)
	}
}

func TestOwnerRateLimitMiddleware_FallsBackToIP(t *testing.T) {
	t.Parallel()
	limiter := NewRateLimiter(100, 1)
	called := 0
	h := OwnerRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }), limiter)

	// No owner_id anywhere — keys by client IP.
	req := httptest.NewRequest(http.MethodGet, "/v1/decisions?subject=x", nil)
	req.RemoteAddr = "1.2.3.4:9000"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if called != 1 {
		t.Errorf("called = %d", called)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second from same IP not limited; code = %d", rr2.Code)
	}
}

func TestOwnerRateLimitMiddleware_SkipsHealthAndMetrics(t *testing.T) {
	t.Parallel()
	limiter := NewRateLimiter(100, 1)
	called := 0
	h := OwnerRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }), limiter)
	for i := 0; i < 5; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/health", nil))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}
	if called != 10 {
		t.Errorf("called = %d, want 10 (no limiting on open paths)", called)
	}
}
