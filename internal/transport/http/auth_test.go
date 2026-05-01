package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerAuthMiddleware_DisabledWhenEmpty(t *testing.T) {
	t.Parallel()
	called := false
	h := BearerAuthMiddleware("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/commitments", nil))
	if !called {
		t.Fatal("handler not called when token empty")
	}
}

func TestBearerAuthMiddleware_RejectsMissingToken(t *testing.T) {
	t.Parallel()
	h := BearerAuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler called")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/commitments", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestBearerAuthMiddleware_AcceptsValidToken(t *testing.T) {
	t.Parallel()
	called := false
	h := BearerAuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/commitments", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !called {
		t.Fatal("handler not called for valid token")
	}
}

func TestBearerAuthMiddleware_RejectsWrongToken(t *testing.T) {
	t.Parallel()
	h := BearerAuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler called")
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/commitments", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestBearerAuthMiddleware_PassesThroughHealth(t *testing.T) {
	t.Parallel()
	called := false
	h := BearerAuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if !called {
		t.Fatal("/v1/health requires bearer token")
	}
}

func TestBearerAuthMiddleware_PassesThroughMetrics(t *testing.T) {
	t.Parallel()
	called := false
	h := BearerAuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !called {
		t.Fatal("/metrics requires bearer token")
	}
}
