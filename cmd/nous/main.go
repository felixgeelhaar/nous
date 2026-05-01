package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/felixgeelhaar/nous/api/nous/v1"
	"github.com/felixgeelhaar/nous/internal/adapters/chronos"
	"github.com/felixgeelhaar/nous/internal/adapters/mnemos"
	"github.com/felixgeelhaar/nous/internal/adapters/praxis"
	"github.com/felixgeelhaar/nous/internal/auth"
	"github.com/felixgeelhaar/nous/internal/config"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/observability"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store"
	"github.com/felixgeelhaar/nous/internal/validation"
	grpcserver "github.com/felixgeelhaar/nous/internal/transport/grpc"
	httpserver "github.com/felixgeelhaar/nous/internal/transport/http"
	"github.com/felixgeelhaar/nous/internal/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// monitorAdapterHealth periodically updates adapter health metrics.
func monitorAdapterHealth(ctx context.Context, metrics *observability.Metrics, mnemosAdptr *mnemos.Adapter, chronosAdptr *chronos.Adapter) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if mnemosAdptr != nil {
				metrics.SetAdapterHealth("mnemos", mnemosAdptr.AdapterStatus())
			}
			if chronosAdptr != nil {
				metrics.SetAdapterHealth("chronos", chronosAdptr.AdapterStatus())
			}
		}
	}
}

func main() {
	logger := slog.New(observability.NewHandler(slog.NewJSONHandler(os.Stdout, nil)))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Observability
	tp, err := observability.InitTracer(ctx, os.Stdout, "nous")
	if err != nil {
		return err
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	metrics := observability.NewMetrics()

	// Persistence
	conn, err := store.Open(ctx, cfg.DBType, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Outbound adapters (optional)
	mnemosAdapter, err := mnemos.NewGRPC(cfg.MnemosAddr, cfg.MnemosTLSCert, cfg.MnemosBearerToken)
	if err != nil {
		return err
	}
	defer func() { _ = mnemosAdapter.Close() }()

	chronosAdapter, err := chronos.NewGRPC(cfg.ChronosAddr, cfg.ChronosTLSCert, cfg.ChronosBearerToken)
	if err != nil {
		return err
	}
	defer func() { _ = chronosAdapter.Close() }()

	// Praxis adapter: stub-disabled when NOUS_PRAXIS_ADDR is unset.
	// When set, the adapter wires gRPC + TLS + bearer-token auth for
	// downstream calls; the Praxis service itself may not be deployed
	// yet, in which case methods return "not implemented" and the
	// evaluator soft-fails.
	praxisAdapter := praxis.NewAdapter(praxis.Config{
		Addr:        cfg.PraxisAddr,
		TLSCertFile: cfg.PraxisTLSCert,
		BearerToken: cfg.PraxisBearerToken,
	})

	// Start adapter health monitoring
	go monitorAdapterHealth(ctx, metrics, mnemosAdapter, chronosAdapter)

	// Engines
	riskCfg := risk.DefaultConfig()
	if cfg.Risk.OverdueWeight > 0 {
		riskCfg.OverdueWeight = cfg.Risk.OverdueWeight
	}
	if cfg.Risk.DueSoonWeight > 0 {
		riskCfg.DueSoonWeight = cfg.Risk.DueSoonWeight
	}
	if cfg.Risk.DueSoonWindow > 0 {
		riskCfg.DueSoonWindow = cfg.Risk.DueSoonWindow
	}
	if cfg.Risk.ConfidenceWeight > 0 {
		riskCfg.ConfidenceWeight = cfg.Risk.ConfidenceWeight
	}
	if cfg.Risk.SignalWeight > 0 {
		riskCfg.SignalWeight = cfg.Risk.SignalWeight
	}
	riskEngine := risk.New(riskCfg)

	ivCfg := intervention.DefaultConfig()
	if cfg.Intervention.NudgeThreshold > 0 {
		ivCfg.NudgeThreshold = cfg.Intervention.NudgeThreshold
	}
	if cfg.Intervention.EscalateThreshold > 0 {
		ivCfg.EscalateThreshold = cfg.Intervention.EscalateThreshold
	}
	if cfg.Intervention.AutomationConfidence > 0 {
		ivCfg.AutomationConfidence = cfg.Intervention.AutomationConfidence
	}
	ivEngine := intervention.New(ivCfg)

	// Pipelines
	commitmentExtractor := buildCommitmentExtractor(cfg.LLM)
	extractor, err := pipeline.NewExtractor(pipeline.ExtractorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Extractor:     commitmentExtractor,
		MinConfidence: cfg.Extract.MinConfidence,
		Metrics:       metrics,
	})
	if err != nil {
		return err
	}

	evaluator, err := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Mnemos:        mnemosAdapter,
		Chronos:       chronosAdapter,
		Praxis:        praxisAdapter,
		Risk:          riskEngine,
		Intervention:  ivEngine,
		Metrics:       metrics,
	})
	if err != nil {
		return err
	}

	// Health checker - checks DB and adapters. Unconfigured adapters
	// (no NOUS_*_ADDR set) are not health-impacting: Nous degrades
	// gracefully without Mnemos or Chronos and the pipeline soft-fails
	// enrichment to nil. Only adapters that were configured but went
	// unhealthy at runtime fail the probe.
	healthCheck := func(ctx context.Context) error {
		// Check database
		if err := conn.Ping(ctx); err != nil {
			return fmt.Errorf("db: %w", err)
		}
		if cfg.MnemosAddr != "" && mnemosAdapter != nil {
			if status := mnemosAdapter.AdapterStatus(); status != "healthy" {
				return fmt.Errorf("mnemos: %s", status)
			}
		}
		if cfg.ChronosAddr != "" && chronosAdapter != nil {
			if status := chronosAdapter.AdapterStatus(); status != "healthy" {
				return fmt.Errorf("chronos: %s", status)
			}
		}
		if cfg.PraxisAddr != "" && praxisAdapter != nil {
			if status := praxisAdapter.AdapterStatus(); status != "healthy" {
				return fmt.Errorf("praxis: %s", status)
			}
		}
		return nil
	}

	// JWT rotator (optional). When NOUS_JWT_ROTATE_PERIOD is set, the
	// rotator owns the active KeySet and runs a background goroutine
	// that rotates the signing key on schedule. The Verifier reads
	// snapshots from the rotator so all transports stay coherent.
	rotator, err := buildJWTRotator(cfg)
	if err != nil {
		return fmt.Errorf("jwt rotator: %w", err)
	}
	var jwtVerifier *auth.Verifier
	if rotator != nil {
		jwtVerifier = auth.NewVerifierFromRotator(rotator)
		go rotator.Run(ctx)
		defer rotator.Stop()
		slog.Info("JWT rotator started", "period", cfg.JWTRotatePeriod, "overlap", cfg.JWTRotateOverlap)
	} else {
		jwtVerifier, err = buildJWTVerifier(cfg)
		if err != nil {
			return fmt.Errorf("jwt: %w", err)
		}
	}

	// gRPC server
	grpcOpts := []grpc.ServerOption{grpc.ChainUnaryInterceptor(
		grpcserver.CombinedAuthInterceptor(cfg.AuthToken, jwtVerifier),
		observability.GRPCUnaryInterceptor,
	)}
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		creds, err := serverTransportCredentials(cfg)
		if err != nil {
			return fmt.Errorf("grpc tls: %w", err)
		}
		grpcOpts = append(grpcOpts, grpc.Creds(creds))
		slog.Info("gRPC TLS enabled", "mtls", cfg.MTLSClientCAFile != "")
	}
	grpcSrv := grpc.NewServer(grpcOpts...)
	nousv1.RegisterNousServer(grpcSrv, grpcserver.NewServer(
		extractor, evaluator,
		conn.Commitments, conn.Decisions, conn.Interventions,
	))

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	defer func() { _ = lis.Close() }()

	grpcErr := make(chan error, 1)
	go func() {
		slog.Info("gRPC server listening", "addr", cfg.GRPCAddr)
		grpcErr <- grpcSrv.Serve(lis)
	}()

	// HTTP server (optional)
	var httpSrv *http.Server
	var httpErr chan error
	if cfg.HTTPAddr != "" {
		validator, verr := buildValidator(cfg.ValidationRules)
		if verr != nil {
			return fmt.Errorf("validation: %w", verr)
		}
		handler := httpserver.NewServer(extractor, evaluator, conn.Commitments, conn.Decisions, conn.Interventions, metrics, healthCheck).WithValidator(validator)
		root := handler.Handler()
		root = observability.HTTPMiddleware(root)
		// Per-owner limiter (caller identity) layered above the IP limiter
		// (raw client). Owners are isolated from each other; IPs are
		// isolated from sharing a single owner.
		root = httpserver.OwnerRateLimitMiddleware(root, httpserver.NewRateLimiter(50, 10))
		root = httpserver.RateLimitMiddleware(root, httpserver.NewRateLimiter(100, 20))
		root = httpserver.CombinedAuthMiddleware(cfg.AuthToken, jwtVerifier, root)
		httpLis, err := net.Listen("tcp", cfg.HTTPAddr)
		if err != nil {
			return err
		}
		defer func() { _ = httpLis.Close() }()

		httpSrv = &http.Server{Handler: root}
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			tlsCfg, err := serverTLSConfig(cfg)
			if err != nil {
				return fmt.Errorf("http tls: %w", err)
			}
			httpSrv.TLSConfig = tlsCfg
		}
		httpErr = make(chan error, 1)
		go func() {
			scheme := "http"
			if httpSrv.TLSConfig != nil {
				scheme = "https"
			}
			slog.Info("HTTP server listening", "addr", cfg.HTTPAddr, "scheme", scheme)
			if httpSrv.TLSConfig != nil {
				httpErr <- httpSrv.ServeTLS(httpLis, cfg.TLSCertFile, cfg.TLSKeyFile)
			} else {
				httpErr <- httpSrv.Serve(httpLis)
			}
		}()
	}

	// Worker
	w := worker.New(evaluator, cfg.TickInterval, slog.Default())
	w.Start(ctx)
	defer w.Stop()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
		slog.Info("shutting down gracefully")
	case err := <-grpcErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
	case err := <-httpErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	grpcSrv.GracefulStop()
	if httpSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown failed", "error", err)
		}
	}
	return nil
}

// buildValidator parses NOUS_VALIDATION_RULES into a validator. The
// env-var format is comma-separated `name=expr` entries. Empty input
// yields a nil validator, which is treated as "no rules".
func buildValidator(rules string) (*validation.Validator, error) {
	rules = strings.TrimSpace(rules)
	if rules == "" {
		return nil, nil
	}
	parts := strings.Split(rules, ",")
	parsed := make([]validation.Rule, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.Index(p, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("validation rule %q must be name=expr", p)
		}
		parsed = append(parsed, validation.Rule{
			Name: strings.TrimSpace(p[:eq]),
			Expr: strings.TrimSpace(p[eq+1:]),
		})
	}
	return validation.NewValidator(parsed)
}

// serverTLSConfig builds a tls.Config for the HTTP server based on
// the server cert/key plus an optional client-CA file for mTLS.
func serverTLSConfig(cfg config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load keypair: %w", err)
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if cfg.MTLSClientCAFile != "" {
		caData, err := os.ReadFile(cfg.MTLSClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("client CA file %s contains no usable certs", cfg.MTLSClientCAFile)
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return tlsCfg, nil
}

// serverTransportCredentials wraps serverTLSConfig in gRPC's
// credentials.TransportCredentials.
func serverTransportCredentials(cfg config.Config) (credentials.TransportCredentials, error) {
	tlsCfg, err := serverTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(tlsCfg), nil
}

// buildJWTRotator returns a Rotator when NOUS_JWT_ROTATE_PERIOD is
// set. Returns nil when rotation is disabled. The bootstrap secret
// comes from cfg.JWTSecretHex when set, else a fresh random secret.
func buildJWTRotator(cfg config.Config) (*auth.Rotator, error) {
	if cfg.JWTRotatePeriod <= 0 {
		return nil, nil
	}
	overlap := cfg.JWTRotateOverlap
	if overlap <= 0 {
		overlap = cfg.JWTDefaultTTL
	}
	if overlap <= 0 {
		overlap = time.Hour
	}
	var seed []byte
	if cfg.JWTSecretHex != "" {
		raw, err := hex.DecodeString(cfg.JWTSecretHex)
		if err != nil {
			return nil, fmt.Errorf("decode NOUS_JWT_SECRET: %w", err)
		}
		seed = raw
	}
	return auth.NewRotator(seed, auth.RotatorConfig{
		Period:  cfg.JWTRotatePeriod,
		Overlap: overlap,
	})
}

// buildJWTVerifier constructs an auth.Verifier from environment-driven
// config. Returns nil when no JWT secret is configured, in which case
// JWT auth is disabled. Hex-decoded secrets are validated to be ≥ 32
// bytes (HS256 standard).
func buildJWTVerifier(cfg config.Config) (*auth.Verifier, error) {
	if cfg.JWTSecretHex == "" && cfg.JWTPrevSecHex == "" {
		return nil, nil
	}
	ks := auth.KeySet{ActiveKID: cfg.JWTKID, PreviousKID: cfg.JWTPrevKID}
	if cfg.JWTSecretHex != "" {
		raw, err := hex.DecodeString(cfg.JWTSecretHex)
		if err != nil {
			return nil, fmt.Errorf("decode NOUS_JWT_SECRET: %w", err)
		}
		if len(raw) < 32 {
			return nil, fmt.Errorf("NOUS_JWT_SECRET must be ≥ 32 bytes (got %d)", len(raw))
		}
		ks.ActiveSecret = raw
	}
	if cfg.JWTPrevSecHex != "" {
		raw, err := hex.DecodeString(cfg.JWTPrevSecHex)
		if err != nil {
			return nil, fmt.Errorf("decode NOUS_JWT_PREV_SECRET: %w", err)
		}
		if len(raw) < 32 {
			return nil, fmt.Errorf("NOUS_JWT_PREV_SECRET must be ≥ 32 bytes (got %d)", len(raw))
		}
		ks.PreviousSecret = raw
	}
	return auth.NewVerifier(ks), nil
}

// buildCommitmentExtractor selects an extractor based on LLM config.
// Empty provider falls back to the deterministic ScriptedExtractor so
// local development needs no API key.
func buildCommitmentExtractor(c config.LLMConfig) llm.CommitmentExtractor {
	switch c.Provider {
	case "anthropic":
		opts := []llm.AnthropicOption{}
		if c.BaseURL != "" {
			opts = append(opts, llm.WithAnthropicBaseURL(c.BaseURL))
		}
		return llm.NewLLMExtractor(llm.NewAnthropicProvider(c.APIKey, c.Model, opts...), llm.WithContextBudget(llm.BudgetFor("anthropic")))
	case "openai":
		opts := []llm.OpenAIOption{}
		if c.BaseURL != "" {
			opts = append(opts, llm.WithOpenAIBaseURL(c.BaseURL))
		}
		return llm.NewLLMExtractor(llm.NewOpenAIProvider(c.APIKey, c.Model, opts...), llm.WithContextBudget(llm.BudgetFor("openai")))
	case "gemini":
		opts := []llm.GeminiOption{}
		if c.BaseURL != "" {
			opts = append(opts, llm.WithGeminiBaseURL(c.BaseURL))
		}
		return llm.NewLLMExtractor(llm.NewGeminiProvider(c.APIKey, c.Model, opts...), llm.WithContextBudget(llm.BudgetFor("gemini")))
	case "bedrock":
		// BaseURL is reused as the AWS region override.
		p, err := llm.NewBedrockProvider(context.Background(), c.BaseURL, c.Model)
		if err != nil {
			slog.Warn("bedrock init failed; falling back to ScriptedExtractor", "err", err)
			return llm.NewScriptedExtractor()
		}
		return llm.NewLLMExtractor(p, llm.WithContextBudget(llm.BudgetFor("bedrock")))
	default:
		return llm.NewScriptedExtractor()
	}
}
