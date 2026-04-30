// Package worker runs the Nous evaluation loop on a configurable tick.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/felixgeelhaar/nous/internal/pipeline"
)

// Worker runs Evaluate on a timer until Stop is called.
type Worker struct {
	evaluator *pipeline.Evaluator
	interval  time.Duration
	logger    *slog.Logger

	done   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	cancel context.CancelFunc
}

// New returns a worker that calls evaluator.Evaluate every interval.
func New(evaluator *pipeline.Evaluator, interval time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		evaluator: evaluator,
		interval:  interval,
		logger:    logger,
		done:      make(chan struct{}),
	}
}

// Start begins the evaluation loop in a background goroutine. It is
// safe to call Start only once; subsequent calls are ignored.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return // already started
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.wg.Add(1)
	go w.loop(ctx)
}

// Stop signals the worker to shut down and blocks until the current
// tick completes (or the context is cancelled).
func (w *Worker) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	w.wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run once immediately on start.
	w.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker shutting down")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	start := time.Now()
	res, err := w.evaluator.Evaluate(ctx, pipeline.EvaluateOptions{})
	if err != nil {
		w.logger.Error("evaluate failed", "error", err)
		return
	}
	w.logger.Info("evaluate completed",
		"evaluated", res.Evaluated,
		"updated", res.Updated,
		"interventions", len(res.Interventions),
		"decisions", len(res.Decisions),
		"duration", time.Since(start),
	)
}

// RunOnce performs a single evaluation synchronously. Useful for
// CLI one-shot mode.
func RunOnce(ctx context.Context, evaluator *pipeline.Evaluator, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	res, err := evaluator.Evaluate(ctx, pipeline.EvaluateOptions{})
	if err != nil {
		return fmt.Errorf("worker: evaluate: %w", err)
	}
	logger.Info("evaluate completed",
		"evaluated", res.Evaluated,
		"updated", res.Updated,
		"interventions", len(res.Interventions),
		"decisions", len(res.Decisions),
	)
	return nil
}
