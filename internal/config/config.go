// Package config loads Nous runtime configuration from environment
// variables with sensible defaults for development.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for Nous.
type Config struct {
	// Server
	GRPCAddr string // e.g. ":50051"
	HTTPAddr string // e.g. ":8080" (optional)

	// Database
	DBType string // memory, sqlite, postgres
	DBDSN  string // connection string or path

	// Worker
	TickInterval time.Duration // evaluation loop tick

	// Risk engine tuning (all optional; zero = use defaults)
	Risk RiskConfig

	// Intervention engine tuning (all optional; zero = use defaults)
	Intervention InterventionConfig

	// LLM extraction tuning
	Extract ExtractConfig

	// LLM provider
	LLM LLMConfig

	// Inbound auth (HTTP + gRPC). Empty disables auth.
	AuthToken string

	// JWT auth (HTTP + gRPC). Active hex secret signs and validates new
	// tokens; previous hex secret (if set) only validates, supporting
	// zero-downtime key rotation. Either may be empty to disable.
	JWTKID         string
	JWTSecretHex   string
	JWTPrevKID     string
	JWTPrevSecHex  string
	JWTDefaultTTL  time.Duration

	// JWT key rotation. Period > 0 enables a background rotator that
	// generates a fresh active key every period and demotes the
	// outgoing one to previous for `overlap` so live tokens stay
	// valid. Overlap should be ≥ JWTDefaultTTL.
	JWTRotatePeriod  time.Duration
	JWTRotateOverlap time.Duration

	// Validation rules. Comma-separated `name=expr` entries.
	// Example: NOUS_VALIDATION_RULES="max_text=text_len <= 5000,owner_prefix=owner_id.startsWith(\"u_\")"
	ValidationRules string

	// External services
	MnemosAddr       string // gRPC address of Mnemos (optional)
	MnemosTLSCert    string // path to TLS cert file (optional)
	MnemosBearerToken string // bearer token for Mnemos auth (optional)
	ChronosAddr      string // gRPC address of Chronos (optional)
	ChronosTLSCert   string // path to TLS cert file (optional)
	ChronosBearerToken string // bearer token for Chronos auth (optional)
	PraxisAddr       string // gRPC address of Praxis (optional)
	PraxisTLSCert    string // path to TLS cert file (optional)
	PraxisBearerToken string // bearer token for Praxis auth (optional)

	// Inbound TLS for the Nous server itself (HTTP + gRPC). When set,
	// the server starts in TLS mode using the provided cert + key.
	// Empty disables — operators relying on a TLS-terminating proxy
	// leave these unset.
	TLSCertFile string
	TLSKeyFile  string
	// MTLSClientCAFile, when set, requires mutual TLS: client certs
	// signed by this CA are required on every connection. Empty
	// keeps the server in regular TLS mode.
	MTLSClientCAFile string
}

// RiskConfig overrides risk engine defaults.
type RiskConfig struct {
	OverdueWeight    float64
	DueSoonWeight    float64
	DueSoonWindow    time.Duration
	ConfidenceWeight float64
	SignalWeight     float64
}

// InterventionConfig overrides intervention engine defaults.
type InterventionConfig struct {
	NudgeThreshold       float64
	EscalateThreshold    float64
	AutomationConfidence float64
}

// ExtractConfig overrides extraction defaults.
type ExtractConfig struct {
	MinConfidence float64
}

// LLMConfig configures the optional LLM provider used for extraction.
// Provider empty means "use the deterministic ScriptedExtractor".
type LLMConfig struct {
	Provider string // anthropic | openai | "" (none)
	APIKey   string
	Model    string // optional override
	BaseURL  string // optional override (for proxies / openai-compat endpoints)
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		GRPCAddr:     getenv("NOUS_GRPC_ADDR", ":50051"),
		HTTPAddr:     os.Getenv("NOUS_HTTP_ADDR"),
		DBType:       getenv("NOUS_DB_TYPE", "sqlite"),
		DBDSN:        getenv("NOUS_DB_DSN", "nous.db"),
		TickInterval: parseDuration(os.Getenv("NOUS_TICK_INTERVAL"), 5*time.Minute),
		MnemosAddr:       os.Getenv("NOUS_MNEMOS_ADDR"),
		MnemosTLSCert:    os.Getenv("NOUS_MNEMOS_TLS_CERT"),
		MnemosBearerToken: os.Getenv("NOUS_MNEMOS_BEARER_TOKEN"),
		ChronosAddr:      os.Getenv("NOUS_CHRONOS_ADDR"),
		ChronosTLSCert:   os.Getenv("NOUS_CHRONOS_TLS_CERT"),
		ChronosBearerToken: os.Getenv("NOUS_CHRONOS_BEARER_TOKEN"),
		PraxisAddr:       os.Getenv("NOUS_PRAXIS_ADDR"),
		PraxisTLSCert:    os.Getenv("NOUS_PRAXIS_TLS_CERT"),
		PraxisBearerToken: os.Getenv("NOUS_PRAXIS_BEARER_TOKEN"),
		TLSCertFile:      os.Getenv("NOUS_TLS_CERT_FILE"),
		TLSKeyFile:       os.Getenv("NOUS_TLS_KEY_FILE"),
		MTLSClientCAFile: os.Getenv("NOUS_MTLS_CLIENT_CA_FILE"),
	}

	// Risk
	cfg.Risk.OverdueWeight = parseFloat(os.Getenv("NOUS_RISK_OVERDUE_WEIGHT"), 0)
	cfg.Risk.DueSoonWeight = parseFloat(os.Getenv("NOUS_RISK_DUE_SOON_WEIGHT"), 0)
	cfg.Risk.DueSoonWindow = parseDuration(os.Getenv("NOUS_RISK_DUE_SOON_WINDOW"), 0)
	cfg.Risk.ConfidenceWeight = parseFloat(os.Getenv("NOUS_RISK_CONFIDENCE_WEIGHT"), 0)
	cfg.Risk.SignalWeight = parseFloat(os.Getenv("NOUS_RISK_SIGNAL_WEIGHT"), 0)

	// Intervention
	cfg.Intervention.NudgeThreshold = parseFloat(os.Getenv("NOUS_NUDGE_THRESHOLD"), 0)
	cfg.Intervention.EscalateThreshold = parseFloat(os.Getenv("NOUS_ESCALATE_THRESHOLD"), 0)
	cfg.Intervention.AutomationConfidence = parseFloat(os.Getenv("NOUS_AUTOMATION_CONFIDENCE"), 0)

	// Extract
	cfg.Extract.MinConfidence = parseFloat(os.Getenv("NOUS_EXTRACT_MIN_CONFIDENCE"), 0)

	// LLM
	cfg.LLM.Provider = strings.ToLower(strings.TrimSpace(os.Getenv("NOUS_LLM_PROVIDER")))
	cfg.LLM.APIKey = os.Getenv("NOUS_LLM_API_KEY")
	cfg.LLM.Model = os.Getenv("NOUS_LLM_MODEL")
	cfg.LLM.BaseURL = os.Getenv("NOUS_LLM_BASE_URL")

	// Auth
	cfg.AuthToken = os.Getenv("NOUS_AUTH_TOKEN")
	cfg.JWTKID = os.Getenv("NOUS_JWT_KID")
	cfg.JWTSecretHex = os.Getenv("NOUS_JWT_SECRET")
	cfg.JWTPrevKID = os.Getenv("NOUS_JWT_PREV_KID")
	cfg.JWTPrevSecHex = os.Getenv("NOUS_JWT_PREV_SECRET")
	cfg.JWTDefaultTTL = parseDuration(os.Getenv("NOUS_JWT_TTL"), time.Hour)
	cfg.JWTRotatePeriod = parseDuration(os.Getenv("NOUS_JWT_ROTATE_PERIOD"), 0)
	cfg.JWTRotateOverlap = parseDuration(os.Getenv("NOUS_JWT_ROTATE_OVERLAP"), 0)
	cfg.ValidationRules = os.Getenv("NOUS_VALIDATION_RULES")

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate ensures required fields are present and sane.
func (c Config) Validate() error {
	if c.GRPCAddr == "" {
		return fmt.Errorf("config: grpc_addr is required")
	}
	switch strings.ToLower(c.DBType) {
	case "memory", "sqlite", "sqlite3", "postgres", "postgresql":
		// ok
	default:
		return fmt.Errorf("config: unsupported db_type %q", c.DBType)
	}
	if c.TickInterval <= 0 {
		return fmt.Errorf("config: tick_interval must be positive")
	}
	switch c.LLM.Provider {
	case "", "anthropic", "openai", "gemini", "bedrock", "ollama":
		// ok
	default:
		return fmt.Errorf("config: unsupported llm provider %q", c.LLM.Provider)
	}
	// Bedrock auth comes from the AWS credential chain; ollama is a
	// local server with no auth — neither requires NOUS_LLM_API_KEY.
	if c.LLM.Provider != "" && c.LLM.Provider != "bedrock" && c.LLM.Provider != "ollama" && c.LLM.APIKey == "" {
		return fmt.Errorf("config: NOUS_LLM_API_KEY required when NOUS_LLM_PROVIDER=%s", c.LLM.Provider)
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(v string, fallback time.Duration) time.Duration {
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func parseFloat(v string, fallback float64) float64 {
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
