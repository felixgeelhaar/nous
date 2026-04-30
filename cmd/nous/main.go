package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/felixgeelhaar/nous/api/nous/v1"
	"github.com/felixgeelhaar/nous/internal/adapters/chronos"
	"github.com/felixgeelhaar/nous/internal/adapters/mnemos"
	"github.com/felixgeelhaar/nous/internal/config"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/observability"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store"
	grpcserver "github.com/felixgeelhaar/nous/internal/transport/grpc"
	httpserver "github.com/felixgeelhaar/nous/internal/transport/http"
	"github.com/felixgeelhaar/nous/internal/worker"
	"google.golang.org/grpc"
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
	extractor, err := pipeline.NewExtractor(pipeline.ExtractorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Extractor:     llm.NewScriptedExtractor(), // MVP: scripted only
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
		Risk:          riskEngine,
		Intervention:  ivEngine,
		Metrics:       metrics,
	})
	if err != nil {
		return err
	}

	// Health checker - checks DB and adapters
	healthCheck := func(ctx context.Context) error {
		// Check database
		if err := conn.Ping(ctx); err != nil {
			return fmt.Errorf("db: %w", err)
		}
		// Check Mnemos adapter
		if mnemosAdapter != nil {
			if status := mnemosAdapter.AdapterStatus(); status != "healthy" {
				return fmt.Errorf("mnemos: %s", status)
			}
		}
		// Check Chronos adapter
		if chronosAdapter != nil {
			if status := chronosAdapter.AdapterStatus(); status != "healthy" {
				return fmt.Errorf("chronos: %s", status)
			}
		}
		return nil
	}

	// gRPC server
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(observability.GRPCUnaryInterceptor),
	)
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
		handler := httpserver.NewServer(extractor, evaluator, conn.Commitments, conn.Decisions, conn.Interventions, metrics, healthCheck)
		root := handler.Handler()
		root = observability.HTTPMiddleware(root)
		root = httpserver.RateLimitMiddleware(root, httpserver.NewRateLimiter(100, 20))
		httpLis, err := net.Listen("tcp", cfg.HTTPAddr)
		if err != nil {
			return err
		}
		defer func() { _ = httpLis.Close() }()

		httpSrv = &http.Server{Handler: root}
		httpErr = make(chan error, 1)
		go func() {
			slog.Info("HTTP server listening", "addr", cfg.HTTPAddr)
			httpErr <- httpSrv.Serve(httpLis)
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
