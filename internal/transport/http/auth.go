package http

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/felixgeelhaar/nous/internal/auth"
)

// BearerAuthMiddleware enforces a static bearer token on protected
// paths. Health (`/v1/health`) and metrics (`/metrics`) stay open so
// the same middleware works in front of probes and scrapers. An empty
// token means "auth disabled" — the middleware passes through.
func BearerAuthMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOpenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			writeAuthErr(w, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CombinedAuthMiddleware accepts either a static bearer token OR a
// JWT verified by `verifier`. The first non-empty option wins per
// request: callers can present either credential. When both options
// are empty/nil, auth is disabled.
//
// Static-token comparison is constant-time. JWT verification covers
// signature, expiry, and issuer.
func CombinedAuthMiddleware(staticToken string, verifier *auth.Verifier, next http.Handler) http.Handler {
	if staticToken == "" && verifier == nil {
		return next
	}
	staticExpected := []byte("Bearer " + staticToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOpenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		raw := r.Header.Get("Authorization")
		if raw == "" {
			writeAuthErr(w, "missing bearer token")
			return
		}
		// Try static-bearer match first when configured.
		if staticToken != "" && subtle.ConstantTimeCompare([]byte(raw), staticExpected) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		// Fall back to JWT verification.
		if verifier != nil && strings.HasPrefix(raw, "Bearer ") {
			if _, err := verifier.Verify(strings.TrimPrefix(raw, "Bearer ")); err == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeAuthErr(w, "missing or invalid bearer token")
	})
}

func writeAuthErr(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func isOpenPath(p string) bool {
	switch {
	case p == "/metrics":
		return true
	case strings.HasPrefix(p, "/v1/health"):
		return true
	}
	return false
}
