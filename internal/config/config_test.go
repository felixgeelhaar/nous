package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GRPCAddr != ":50051" {
		t.Errorf("grpc_addr = %q, want :50051", cfg.GRPCAddr)
	}
	if cfg.DBType != "sqlite" {
		t.Errorf("db_type = %q, want sqlite", cfg.DBType)
	}
	if cfg.TickInterval != 5*time.Minute {
		t.Errorf("tick_interval = %v, want 5m", cfg.TickInterval)
	}
}

func TestLoad_WithEnvVars(t *testing.T) {
	t.Setenv("NOUS_GRPC_ADDR", ":9999")
	t.Setenv("NOUS_DB_TYPE", "memory")
	t.Setenv("NOUS_TICK_INTERVAL", "10m")
	t.Setenv("NOUS_HTTP_ADDR", ":8080")
	t.Setenv("NOUS_EXTRACT_MIN_CONFIDENCE", "0.5")
	t.Setenv("NOUS_RISK_OVERDUE_WEIGHT", "0.7")
	t.Setenv("NOUS_NUDGE_THRESHOLD", "0.6")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GRPCAddr != ":9999" {
		t.Errorf("grpc_addr = %q, want :9999", cfg.GRPCAddr)
	}
	if cfg.DBType != "memory" {
		t.Errorf("db_type = %q, want memory", cfg.DBType)
	}
	if cfg.TickInterval != 10*time.Minute {
		t.Errorf("tick_interval = %v, want 10m", cfg.TickInterval)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("http_addr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.Extract.MinConfidence != 0.5 {
		t.Errorf("min_confidence = %v, want 0.5", cfg.Extract.MinConfidence)
	}
	if cfg.Risk.OverdueWeight != 0.7 {
		t.Errorf("overdue_weight = %v, want 0.7", cfg.Risk.OverdueWeight)
	}
	if cfg.Intervention.NudgeThreshold != 0.6 {
		t.Errorf("nudge_threshold = %v, want 0.6", cfg.Intervention.NudgeThreshold)
	}
}

func TestLoad_InvalidTickInterval(t *testing.T) {
	t.Setenv("NOUS_TICK_INTERVAL", "invalid")
	// Should fall back to default and not error
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.TickInterval != 5*time.Minute {
		t.Errorf("tick_interval fallback = %v, want 5m", cfg.TickInterval)
	}
}

func TestLoad_InvalidFloat(t *testing.T) {
	t.Setenv("NOUS_RISK_OVERDUE_WEIGHT", "not-a-number")
	// Should fall back to 0
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Risk.OverdueWeight != 0 {
		t.Errorf("overdue_weight fallback = %v, want 0", cfg.Risk.OverdueWeight)
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  config.Config
		ok   bool
	}{
		{"valid", config.Config{GRPCAddr: ":50051", DBType: "sqlite", TickInterval: time.Minute}, true},
		{"empty grpc", config.Config{GRPCAddr: "", DBType: "sqlite", TickInterval: time.Minute}, false},
		{"bad db", config.Config{GRPCAddr: ":50051", DBType: "mongo", TickInterval: time.Minute}, false},
		{"zero tick", config.Config{GRPCAddr: ":50051", DBType: "sqlite", TickInterval: 0}, false},
		{"postgres", config.Config{GRPCAddr: ":50051", DBType: "postgres", TickInterval: time.Minute}, true},
		{"postgresql", config.Config{GRPCAddr: ":50051", DBType: "postgresql", TickInterval: time.Minute}, true},
		{"sqlite3", config.Config{GRPCAddr: ":50051", DBType: "sqlite3", TickInterval: time.Minute}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoad_UnsupportedDBType(t *testing.T) {
	t.Setenv("NOUS_DB_TYPE", "mongo")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for unsupported db_type")
	}
}

func TestLoad_InvalidEnvPriority(t *testing.T) {
	// Set env var, then clear it to test fallback
	_ = os.Setenv("NOUS_GRPC_ADDR", ":7777")
	defer func() { _ = os.Unsetenv("NOUS_GRPC_ADDR") }()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GRPCAddr != ":7777" {
		t.Errorf("env override = %q, want :7777", cfg.GRPCAddr)
	}
}
