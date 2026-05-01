// Package auth provides JWT token issuance and verification for the
// Nous HTTP and gRPC surfaces. Tokens are HS256 signed with a shared
// secret loaded from configuration; the same Verifier serves both
// transports so a single token works for either.
//
// Two parallel signing keys are supported to enable zero-downtime
// rotation: tokens issued under either KID validate, but new tokens
// always use the active key. To rotate, set the new key as active
// (KID=current) and keep the old key as previous (KID=previous);
// after the longest token TTL has elapsed, drop the previous key.
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is the canonical iss claim value.
const Issuer = "nous"

// Claims is the typed claim set Nous tokens carry.
type Claims struct {
	Subject string   `json:"sub"`
	Scopes  []string `json:"scopes,omitempty"`
	jwt.RegisteredClaims
}

// KeySet holds the active and (optional) previous signing keys.
// During rotation, both keys validate; the active key signs.
type KeySet struct {
	ActiveKID    string
	ActiveSecret []byte
	// Previous is optional; when set, tokens issued under it still
	// validate so a rotation can complete without invalidating live
	// tokens.
	PreviousKID    string
	PreviousSecret []byte
}

// Issuer issues new JWTs.
type Issuer_ struct {
	keys KeySet
	ttl  time.Duration
}

// NewIssuer builds a token issuer. Tokens default to the supplied TTL
// when [IssueToken] is called without an override.
func NewIssuer(keys KeySet, ttl time.Duration) *Issuer_ {
	return &Issuer_{keys: keys, ttl: ttl}
}

// Issue mints a new JWT for sub with the given scopes. ttl overrides
// the issuer default when non-zero.
func (i *Issuer_) Issue(subject string, scopes []string, ttl time.Duration) (string, error) {
	if subject == "" {
		return "", errors.New("auth: subject required")
	}
	if len(i.keys.ActiveSecret) == 0 {
		return "", errors.New("auth: no active signing key")
	}
	if ttl <= 0 {
		ttl = i.ttl
	}
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}
	now := time.Now().UTC()
	claims := &Claims{
		Subject: subject,
		Scopes:  scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if i.keys.ActiveKID != "" {
		tok.Header["kid"] = i.keys.ActiveKID
	}
	return tok.SignedString(i.keys.ActiveSecret)
}

// Verifier verifies JWTs against an active and (optional) previous
// signing key. Safe for concurrent use after construction.
//
// When constructed with NewVerifierFromRotator, the verifier reads
// keys from the rotator on every call so a rotation is picked up
// without re-wiring the verifier.
type Verifier struct {
	keys    KeySet
	rotator *Rotator
}

// keysetFor returns the active key set. Rotator-backed verifiers read
// the live snapshot; statically-keyed verifiers return their own.
func (v *Verifier) keysetFor() KeySet {
	if v.rotator != nil {
		return v.rotator.Snapshot()
	}
	return v.keys
}

// NewVerifier builds a verifier. At least one key must be set or
// every Verify call returns an error.
func NewVerifier(keys KeySet) *Verifier {
	return &Verifier{keys: keys}
}

// NewVerifierFromRotator builds a verifier whose key set is read from
// the rotator on every Verify call. This is the right wiring when
// the active key changes during the process lifetime.
func NewVerifierFromRotator(r *Rotator) *Verifier {
	return &Verifier{rotator: r}
}

// Verify parses tokenStr, validates its signature against the
// configured key set, and checks the standard time-based claims.
// Returns the decoded claims on success.
func (v *Verifier) Verify(tokenStr string) (*Claims, error) {
	if tokenStr == "" {
		return nil, errors.New("auth: empty token")
	}
	keys := v.keysetFor()
	if len(keys.ActiveSecret) == 0 && len(keys.PreviousSecret) == 0 {
		return nil, errors.New("auth: no keys configured")
	}
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("auth: unexpected signing method %q", t.Method.Alg())
		}
		kid, _ := t.Header["kid"].(string)
		switch kid {
		case keys.ActiveKID:
			return keys.ActiveSecret, nil
		case keys.PreviousKID:
			if len(keys.PreviousSecret) == 0 {
				return nil, errors.New("auth: previous key not configured")
			}
			return keys.PreviousSecret, nil
		case "":
			// No KID — try active first, then previous via the
			// secondary parse path below. Most issuers will write a
			// KID; bare tokens are accepted as a convenience for
			// out-of-band issuance.
			return keys.ActiveSecret, nil
		default:
			return nil, fmt.Errorf("auth: unknown key id %q", kid)
		}
	})
	if err != nil {
		// Retry against previous key when the token has no KID and the
		// active-key parse failed for a signature reason.
		if keys.PreviousSecret != nil && strings.Contains(err.Error(), "signature is invalid") {
			parsed, err = jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
				return keys.PreviousSecret, nil
			})
		}
		if err != nil {
			return nil, fmt.Errorf("auth: parse: %w", err)
		}
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("auth: invalid token")
	}
	if claims.Issuer != "" && claims.Issuer != Issuer {
		return nil, fmt.Errorf("auth: unexpected issuer %q", claims.Issuer)
	}
	return claims, nil
}

// HasScope reports whether the claim set grants the requested scope.
// "*" in claims.Scopes is a wildcard.
func (c *Claims) HasScope(want string) bool {
	for _, s := range c.Scopes {
		if s == "*" || s == want {
			return true
		}
	}
	return false
}
