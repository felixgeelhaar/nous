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

	// External services
	MnemosAddr       string // gRPC address of Mnemos (optional)
	MnemosTLSCert    string // path to TLS cert file (optional)
	MnemosBearerToken string // bearer token for Mnemos auth (optional)
	ChronosAddr      string // gRPC address of Chronos (optional)
	ChronosTLSCert   string // path to TLS cert file (optional)
	ChronosBearerToken string // bearer token for Chronos auth (optional)
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
