package auth

import (
	"strings"
	"testing"
	"time"
)

func newKS(active, prev string) KeySet {
	ks := KeySet{ActiveKID: "v1", ActiveSecret: []byte(active)}
	if prev != "" {
		ks.PreviousKID = "v0"
		ks.PreviousSecret = []byte(prev)
	}
	return ks
}

func TestIssuerVerifier_RoundTrip(t *testing.T) {
	t.Parallel()
	ks := newKS("active-secret", "")
	tok, err := NewIssuer(ks, time.Hour).Issue("alice", []string{"extract:write"}, 0)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := NewVerifier(ks).Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("subject = %q", claims.Subject)
	}
	if !claims.HasScope("extract:write") {
		t.Error("missing scope")
	}
}

func TestVerifier_RotationDualKey(t *testing.T) {
	t.Parallel()
	prevKS := newKS("old-secret", "")
	tokOld, _ := NewIssuer(prevKS, time.Hour).Issue("alice", nil, 0)

	// Now rotate: new active, old kept as previous.
	rotated := KeySet{
		ActiveKID:      "v2",
		ActiveSecret:   []byte("new-secret"),
		PreviousKID:    "v1",
		PreviousSecret: []byte("old-secret"),
	}
	v := NewVerifier(rotated)
	if _, err := v.Verify(tokOld); err != nil {
		t.Errorf("token issued under previous key should still validate: %v", err)
	}

	tokNew, _ := NewIssuer(rotated, time.Hour).Issue("alice", nil, 0)
	if _, err := v.Verify(tokNew); err != nil {
		t.Errorf("new token failed: %v", err)
	}
}

func TestVerifier_RejectsExpired(t *testing.T) {
	t.Parallel()
	ks := newKS("s", "")
	tok, _ := NewIssuer(ks, time.Hour).Issue("alice", nil, 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	_, err := NewVerifier(ks).Verify(tok)
	if err == nil {
		t.Fatal("want error on expired token")
	}
}

func TestVerifier_RejectsWrongSignature(t *testing.T) {
	t.Parallel()
	ks1 := newKS("a", "")
	ks2 := newKS("b", "")
	tok, _ := NewIssuer(ks1, time.Hour).Issue("alice", nil, 0)
	_, err := NewVerifier(ks2).Verify(tok)
	if err == nil {
		t.Fatal("want error on wrong-signature token")
	}
}

func TestVerifier_RejectsBadAlg(t *testing.T) {
	t.Parallel()
	// Manually craft an unsigned token that claims alg=none.
	unsigned := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJhbGljZSJ9."
	_, err := NewVerifier(newKS("s", "")).Verify(unsigned)
	if err == nil {
		t.Fatal("want error on alg=none")
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "method") {
		t.Errorf("err = %v", err)
	}
}

func TestIssuer_RejectsEmptySubject(t *testing.T) {
	t.Parallel()
	_, err := NewIssuer(newKS("s", ""), time.Hour).Issue("", nil, 0)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestClaims_WildcardScope(t *testing.T) {
	t.Parallel()
	c := &Claims{Scopes: []string{"*"}}
	if !c.HasScope("anything") {
		t.Error("wildcard not honoured")
	}
}
