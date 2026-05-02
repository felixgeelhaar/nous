package main

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/config"
	"github.com/felixgeelhaar/nous/internal/llm"
)

func TestBuildValidator_Empty(t *testing.T) {
	t.Parallel()
	v, err := buildValidator("")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != nil {
		t.Errorf("empty rules should produce nil validator, got %v", v)
	}
}

func TestBuildValidator_RejectsMalformed(t *testing.T) {
	t.Parallel()
	if _, err := buildValidator("missing-equals"); err == nil {
		t.Fatal("want error on entry without '='")
	}
}

func TestBuildValidator_ParsesRules(t *testing.T) {
	t.Parallel()
	v, err := buildValidator("max=text_len <= 100,prefix=owner_id.startsWith(\"u_\")")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v == nil {
		t.Fatal("validator nil")
	}
}

func TestBuildJWTRotator_DisabledByDefault(t *testing.T) {
	t.Parallel()
	r, err := buildJWTRotator(config.Config{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r != nil {
		t.Error("rotator should be nil with no period")
	}
}

func TestBuildJWTRotator_BootstrapFromHexSecret(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("a", 64) // 32 bytes hex-encoded
	cfg := config.Config{
		JWTSecretHex:    secret,
		JWTRotatePeriod: time.Hour,
		JWTDefaultTTL:   30 * time.Minute,
	}
	r, err := buildJWTRotator(cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r == nil {
		t.Fatal("rotator nil")
	}
	r.Stop()
}

func TestBuildJWTRotator_RejectsInvalidHex(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		JWTSecretHex:    "not-hex",
		JWTRotatePeriod: time.Hour,
	}
	if _, err := buildJWTRotator(cfg); err == nil {
		t.Fatal("want error")
	}
}

func TestBuildJWTVerifier_DisabledWhenEmpty(t *testing.T) {
	t.Parallel()
	v, err := buildJWTVerifier(config.Config{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != nil {
		t.Error("verifier should be nil with no secret")
	}
}

func TestBuildJWTVerifier_HappyPath(t *testing.T) {
	t.Parallel()
	secretHex := hex.EncodeToString(make([]byte, 32))
	cfg := config.Config{JWTSecretHex: secretHex, JWTKID: "k1"}
	v, err := buildJWTVerifier(cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v == nil {
		t.Fatal("verifier nil")
	}
}

func TestBuildJWTVerifier_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	cfg := config.Config{JWTSecretHex: hex.EncodeToString(make([]byte, 16))}
	if _, err := buildJWTVerifier(cfg); err == nil {
		t.Fatal("want error on <32-byte secret")
	}
}

func TestBuildJWTVerifier_RejectsInvalidPrev(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		JWTSecretHex:  hex.EncodeToString(make([]byte, 32)),
		JWTPrevSecHex: "not-hex",
	}
	if _, err := buildJWTVerifier(cfg); err == nil {
		t.Fatal("want error on invalid previous hex")
	}
}

func TestBuildCommitmentExtractor_DefaultsToScripted(t *testing.T) {
	t.Parallel()
	got := buildCommitmentExtractor(config.LLMConfig{})
	if got == nil {
		t.Fatal("nil")
	}
	if !strings.Contains(fmt.Sprintf("%T", got), "ScriptedExtractor") {
		t.Errorf("not scripted: %T", got)
	}
}

func TestBuildCommitmentExtractor_AllProviders(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"anthropic", "openai", "gemini"} {
		c := config.LLMConfig{Provider: provider, APIKey: "k", Model: "m"}
		got := buildCommitmentExtractor(c)
		if _, ok := got.(*llm.LLMExtractor); !ok {
			t.Errorf("%s: not LLMExtractor: %T", provider, got)
		}
	}
}

func TestBuildCommitmentExtractor_BedrockReturnsExtractor(t *testing.T) {
	t.Parallel()
	c := config.LLMConfig{Provider: "bedrock"}
	// NewBedrockProvider may succeed (AWS sdk doesn't connect at
	// construction) or fall back to ScriptedExtractor on init failure;
	// either way the dispatch returns a non-nil extractor.
	got := buildCommitmentExtractor(c)
	if got == nil {
		t.Fatal("nil")
	}
}
