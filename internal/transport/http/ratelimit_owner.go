package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// OwnerRateLimitMiddleware applies a per-owner rate limit on top of
// (or instead of) the IP-based limiter. The owner is extracted from:
//
//  1. `owner_id` query parameter (any GET endpoint that accepts it),
//  2. `X-Nous-Owner` request header,
//  3. JSON body field `owner_id` for POST endpoints,
//  4. fallback to client IP when no owner is identifiable.
//
// Endpoints that are open to bearer-auth bypass (`/v1/health`,
// `/metrics`) are not rate-limited.
func OwnerRateLimitMiddleware(next http.Handler, limiter *RateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOpenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		key, err := ownerKey(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !limiter.Allow(key) {
			w.Header().Set("Retry-After", "1")
			httpError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ownerKey(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.URL.Query().Get("owner_id")); v != "" {
		return "owner:" + v, nil
	}
	if v := strings.TrimSpace(r.Header.Get("X-Nous-Owner")); v != "" {
		return "owner:" + v, nil
	}
	if r.Method == http.MethodPost && r.Body != nil {
		buf, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB peek
		if err != nil {
			return "", err
		}
		r.Body = io.NopCloser(bytes.NewReader(buf))
		var probe struct {
			OwnerID string `json:"owner_id"`
		}
		if json.Unmarshal(buf, &probe) == nil {
			if v := strings.TrimSpace(probe.OwnerID); v != "" {
				return "owner:" + v, nil
			}
		}
	}
	return "ip:" + clientIP(r), nil
}
