// Package praxis is the HTTP adapter for the Praxis execution layer.
// Praxis exposes a small REST surface (`ListCapabilities`, `Execute`,
// `DryRun`) per its public contract; this package wraps that in the
// `ports.PraxisClient` interface Nous's pipeline depends on.
//
// Behaviour:
//
//   - With no `Addr` configured, NewAdapter returns an adapter whose
//     methods all return ErrPraxisDisabled. Pipelines should treat
//     this as "execution is unavailable", not as an outage.
//   - With `Addr` configured, the adapter speaks to Praxis over HTTP
//     and surfaces upstream failures verbatim. Bearer auth is opt-in;
//     TLS-cert pinning is opt-in.
package praxis

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/felixgeelhaar/nous/internal/circuit"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
)

// ErrPraxisDisabled is returned when the adapter has no address
// configured. The pipeline should treat this as "execute is not
// available", not as an outage.
var ErrPraxisDisabled = errors.New("praxis: adapter disabled (no addr)")

// Adapter implements ports.PraxisClient over Praxis's HTTP API.
type Adapter struct {
	cfg     Config
	http    *http.Client
	breaker *circuit.Breaker

	mu     sync.RWMutex
	health string // "healthy" | "degraded" | "disabled"
}

// Config holds the wiring options for the adapter.
type Config struct {
	// Addr is the Praxis HTTP base URL (e.g. "https://praxis.local:8080").
	// Empty disables the adapter.
	Addr string
	// TLSCertFile is an optional path to a PEM file containing the CA
	// certs used to verify the Praxis server. Empty falls back to the
	// system trust store.
	TLSCertFile string
	// BearerToken is an optional shared secret sent in the
	// `Authorization: Bearer <token>` header.
	BearerToken string
	// CallTimeout is the per-call deadline. Zero defaults to 30 seconds.
	CallTimeout time.Duration
}

// NewAdapter constructs an adapter. With cfg.Addr empty, the adapter
// is "disabled" — calls return ErrPraxisDisabled and AdapterStatus
// reports "disabled".
func NewAdapter(cfg Config) *Adapter {
	a := &Adapter{cfg: cfg}
	if cfg.Addr == "" {
		a.health = "disabled"
		return a
	}
	timeout := cfg.CallTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := &http.Transport{}
	if cfg.TLSCertFile != "" {
		tlsCfg, err := loadTLSCert(cfg.TLSCertFile)
		if err == nil {
			transport.TLSClientConfig = tlsCfg
		}
	}
	a.http = &http.Client{Timeout: timeout, Transport: transport}
	a.breaker = circuit.New(circuit.Config{MaxFailures: 3})
	a.health = "healthy"
	return a
}

func loadTLSCert(path string) (*tls.Config, error) {
	pem, err := os.ReadFile(path) //nolint:gosec // operator-supplied cert path
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("praxis: TLS cert file contains no usable certs")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}, nil
}

// AdapterStatus reports the adapter's current health for the
// `/v1/health` aggregator.
func (a *Adapter) AdapterStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.health
}

// ListCapabilities fetches the Praxis capability catalog.
func (a *Adapter) ListCapabilities(ctx context.Context) ([]ports.Capability, error) {
	if a.cfg.Addr == "" {
		return nil, ErrPraxisDisabled
	}
	var body struct {
		Capabilities []capabilityWire `json:"capabilities"`
	}
	if err := a.do(ctx, http.MethodGet, "/v1/capabilities", nil, &body); err != nil {
		return nil, err
	}
	out := make([]ports.Capability, len(body.Capabilities))
	for i, c := range body.Capabilities {
		schema, _ := c.InputSchema.(map[string]any)
		out[i] = ports.Capability{
			Name:        c.Name,
			Description: c.Description,
			InputSchema: schema,
		}
	}
	return out, nil
}

// DryRun simulates a request against Praxis. Praxis validates the
// payload, runs the policy engine, and reports a Simulation without
// making state changes.
func (a *Adapter) DryRun(ctx context.Context, req domain.ActionRequest) (ports.SimulationResult, error) {
	if a.cfg.Addr == "" {
		return ports.SimulationResult{}, ErrPraxisDisabled
	}
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	action := actionFromRequest(req)
	var sim simulationWire
	path := "/v1/actions/" + req.ID.String() + "/dry-run"
	if err := a.do(ctx, http.MethodPost, path, action, &sim); err != nil {
		return ports.SimulationResult{}, err
	}
	predictions := make([]string, 0)
	if sim.Reversible {
		predictions = append(predictions, "reversible")
	}
	for k, v := range sim.Preview {
		predictions = append(predictions, fmt.Sprintf("%s=%v", k, v))
	}
	return ports.SimulationResult{
		Capability:  req.Capability,
		Predictions: predictions,
		Blockers:    sim.Validation.Errors,
	}, nil
}

// Execute submits the request to Praxis for synchronous execution.
// IdempotencyKey is auto-generated when absent.
func (a *Adapter) Execute(ctx context.Context, req domain.ActionRequest) (ports.ExecutionResult, error) {
	if a.cfg.Addr == "" {
		return ports.ExecutionResult{}, ErrPraxisDisabled
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.New().String()
	}
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	action := actionFromRequest(req)
	var res resultWire
	if err := a.do(ctx, http.MethodPost, "/v1/actions", action, &res); err != nil {
		return ports.ExecutionResult{}, err
	}
	out := ports.ExecutionResult{
		Capability:     req.Capability,
		Success:        res.Status == "succeeded",
		Output:         res.Output,
		IdempotencyKey: req.IdempotencyKey,
		ExecutedAt:     res.CompletedAt,
	}
	if res.Error != nil {
		out.Error = res.Error.Message
	}
	return out, nil
}

// do is the shared request helper: encodes body, attaches auth, runs
// through the circuit breaker, decodes JSON response.
func (a *Adapter) do(ctx context.Context, method, path string, body, out any) error {
	url := strings.TrimRight(a.cfg.Addr, "/") + path
	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("praxis: encode body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("praxis: build request: %w", err)
	}
	if a.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.BearerToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	var httpResp *http.Response
	cbErr := a.breaker.Call(ctx, func() error {
		r, err := a.http.Do(req)
		if err != nil {
			return err
		}
		httpResp = r
		return nil
	})
	if cbErr != nil {
		a.mu.Lock()
		a.health = "degraded"
		a.mu.Unlock()
		return fmt.Errorf("praxis: %s %s: %w", method, path, cbErr)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		raw, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("praxis: %s %s: %d %s", method, path, httpResp.StatusCode, strings.TrimSpace(string(raw)))
	}
	a.mu.Lock()
	a.health = "healthy"
	a.mu.Unlock()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(httpResp.Body).Decode(out); err != nil {
		return fmt.Errorf("praxis: decode response: %w", err)
	}
	return nil
}

// actionFromRequest maps Nous's ports.ActionRequest to Praxis's wire
// shape. We deliberately don't import praxis/internal/domain — the
// types are reproduced as `actionWire` etc. so Nous stays decoupled
// from Praxis's internal package layout.
func actionFromRequest(req domain.ActionRequest) actionWire {
	return actionWire{
		ID:             req.ID.String(),
		Capability:     req.Capability,
		Payload:        req.Payload,
		IdempotencyKey: req.IdempotencyKey,
		Mode:           "sync",
	}
}

// --- wire types (mirror Praxis's domain.* JSON shapes) ---

type actionWire struct {
	ID             string         `json:"id"`
	Capability     string         `json:"capability"`
	Payload        map[string]any `json:"payload"`
	IdempotencyKey string         `json:"idempotency_key"`
	Mode           string         `json:"mode"`
}

type capabilityWire struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type resultWire struct {
	ActionID    string         `json:"action_id"`
	Status      string         `json:"status"`
	Output      map[string]any `json:"output"`
	ExternalID  string         `json:"external_id"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt time.Time      `json:"completed_at"`
	Attempts    int            `json:"attempts"`
	Error       *errorWire     `json:"error,omitempty"`
}

type errorWire struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type simulationWire struct {
	ActionID   string         `json:"action_id"`
	Validation validationWire `json:"validation"`
	Preview    map[string]any `json:"preview"`
	Reversible bool           `json:"reversible"`
}

type validationWire struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// Compile-time interface satisfaction check.
var _ ports.PraxisClient = (*Adapter)(nil)
