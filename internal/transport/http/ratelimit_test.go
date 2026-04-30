package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimiter_Allow(t *testing.T) {
	// 2 per second, burst of 2
	rl := NewRateLimiter(2, 2)
	require.True(t, rl.Allow("a"))
	require.True(t, rl.Allow("a"))
	require.False(t, rl.Allow("a")) // bucket empty
	require.True(t, rl.Allow("b"))  // different key
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(100, 1) // 100/s, burst 1
	require.True(t, rl.Allow("x"))
	require.False(t, rl.Allow("x"))

	time.Sleep(20 * time.Millisecond) // ~2 tokens refill at 100/s
	require.True(t, rl.Allow("x"))
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	rl.Allow("old")
	rl.Cleanup(0) // immediately remove stale
	require.Len(t, rl.buckets, 0)
}

func TestRateLimitMiddleware(t *testing.T) {
	// Allow 1 request per second, burst of 1
	rl := NewRateLimiter(1, 1)
	handler := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), rl)

	// First request passes
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	require.Equal(t, http.StatusOK, rr1.Code)

	// Second request from same IP is rate limited
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = req1.RemoteAddr
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	require.Equal(t, http.StatusTooManyRequests, rr2.Code)
	require.Equal(t, "1", rr2.Header().Get("Retry-After"))
}

func TestRateLimitMiddleware_DifferentIPs(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	handler := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), rl)

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "1.2.3.4:1234"
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	require.Equal(t, http.StatusOK, rr1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "5.6.7.8:5678"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	require.Equal(t, http.StatusOK, rr2.Code)
}
