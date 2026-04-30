package coordination

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	axi "github.com/felixgeelhaar/axi-go"
	"github.com/felixgeelhaar/axi-go/domain"
	"github.com/felixgeelhaar/bolt"
	nousdomain "github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

// AxiKernel wraps every Praxis call in an axi-go kernel so that:
//   - effect gating runs (write-external actions require approval
//     when an approver is configured);
//   - budgets are enforced (max duration, max invocation count);
//   - every execution writes a tamper-evident evidence chain we can
//     project into Mnemos.
//
// The kernel registers a single action — "nous-execute-intervention"
// — whose executor closes over the Praxis client. A new instance is
// returned by NewAxiKernel per Nous process; the kernel is safe to
// share across goroutines.
//
// axi-go's action-name regex disallows underscores, so any
// capability name with `_` is mapped to `-` at the boundary. The
// raw capability name is preserved on the request and forwarded to
// Praxis unchanged.
type AxiKernel struct {
	kernel *axi.Kernel
	praxis ports.PraxisClient
	logger *bolt.Logger
}

const (
	axiActionName    = "nous-execute-intervention"
	axiExecutorRef   = "exec.execute_intervention"
	axiPluginName    = "nous.coordination"
	axiEvidenceKind  = "nous.action.executed"
	axiEvidenceError = "nous.action.error"
)

// NewAxiKernel constructs an axi-go-backed Kernel. The bolt logger
// is required so the kernel's internal events land in the same
// JSON stream as the rest of Nous; passing nil falls back to a
// no-op logger via bolt's discard handler equivalent (we keep the
// logger required to avoid silently swallowing audit information).
func NewAxiKernel(praxis ports.PraxisClient, logger *bolt.Logger) (*AxiKernel, error) {
	if err := requirePraxis(praxis); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, errors.New("coordination: bolt logger required")
	}

	plugin, err := newPlugin()
	if err != nil {
		return nil, fmt.Errorf("coordination: build plugin: %w", err)
	}
	kernel := axi.New().
		WithLogger(boltAxiLogger{logger: logger}).
		WithDomainEventPublisher(boltAxiPublisher{logger: logger}).
		WithBudget(axiBudgetFromEnv())
	kernel.RegisterActionExecutor(axiExecutorRef, &executor{praxis: praxis})
	if err := kernel.RegisterPlugin(plugin); err != nil {
		return nil, fmt.Errorf("coordination: register plugin: %w", err)
	}
	return &AxiKernel{kernel: kernel, praxis: praxis, logger: logger}, nil
}

// Kernel returns the underlying axi-go kernel for callers that need
// to drive approvals or list pending actions through axi-go's own
// surface (e.g. the MCP server). The kernel is safe to share.
func (k *AxiKernel) Kernel() *axi.Kernel { return k.kernel }

// Execute runs iv.SuggestedAction through axi-go. The kernel's
// effect/budget machinery applies before the executor runs; if a
// gate trips, the returned Outcome has Success=false and an error
// evidence record so callers can persist the rejection alongside
// the original intervention.
func (k *AxiKernel) Execute(ctx context.Context, iv nousdomain.Intervention) (Outcome, error) {
	if iv.SuggestedAction == nil {
		return Outcome{InterventionID: iv.ID}, ErrNoSuggestedAction
	}
	req := *iv.SuggestedAction
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = iv.ID.String()
	}

	payload := map[string]any{
		"intervention_id": iv.ID.String(),
		"capability":      req.Capability,
		"payload":         req.Payload,
		"idempotency_key": req.IdempotencyKey,
		"constraints":     req.Constraints,
	}

	res, err := k.kernel.Execute(ctx, axi.Invocation{
		Action: axiActionName,
		Input:  payload,
	})
	if err != nil {
		return Outcome{
			InterventionID: iv.ID,
			ActionRequest:  req,
			Success:        false,
			ErrorMessage:   err.Error(),
			Evidence: []EvidenceRecord{{
				Kind:   axiEvidenceError,
				Source: axiPluginName,
				Value:  map[string]any{"capability": req.Capability, "error": err.Error()},
			}},
		}, fmt.Errorf("coordination: axi execute: %w", err)
	}

	// External effects pause at AwaitingApproval. The intervention
	// has already been accepted by the user (or is an automation
	// that ships pre-approved by Nous policy), so we resume by
	// approving with a service principal. A future operator-driven
	// approval flow would replace this auto-approve with a wait on
	// an approval channel.
	if res != nil && res.RequiresApproval {
		approved, approveErr := k.kernel.Approve(ctx, string(res.SessionID), domain.ApprovalDecision{
			Principal: "nous.coordination",
			Rationale: fmt.Sprintf("intervention %s accepted; capability=%s", iv.ID, req.Capability),
		})
		if approveErr != nil {
			return Outcome{
				InterventionID: iv.ID,
				ActionRequest:  req,
				Success:        false,
				ErrorMessage:   approveErr.Error(),
				Evidence: []EvidenceRecord{{
					Kind:   axiEvidenceError,
					Source: axiPluginName,
					Value:  map[string]any{"capability": req.Capability, "error": approveErr.Error()},
				}},
			}, fmt.Errorf("coordination: axi approve: %w", approveErr)
		}
		res = approved
	}
	if res.Failure != nil {
		return Outcome{
			InterventionID: iv.ID,
			ActionRequest:  req,
			Success:        false,
			ErrorMessage:   fmt.Sprintf("%s: %s", res.Failure.Code, res.Failure.Message),
			Evidence: evidenceFrom(res.Evidence, axiEvidenceError, axiPluginName),
		}, nil
	}

	out := Outcome{
		InterventionID: iv.ID,
		ActionRequest:  req,
		Success:        true,
		Evidence:       evidenceFrom(res.Evidence, axiEvidenceKind, axiPluginName),
	}
	if res.Result != nil {
		if data, ok := res.Result.Data.(map[string]any); ok {
			out.Output = data
		} else if res.Result.Data != nil {
			// Round-trip via JSON to map for transport. The slow
			// path is bounded by the size of the executor's
			// response payload — typically tiny.
			b, mErr := json.Marshal(res.Result.Data)
			if mErr == nil {
				m := map[string]any{}
				if uErr := json.Unmarshal(b, &m); uErr == nil {
					out.Output = m
				}
			}
		}
	}
	return out, nil
}

// executor is the action implementation registered with axi-go. It
// receives the input map produced in Execute(), reconstructs the
// nous ActionRequest, and forwards it to Praxis.
type executor struct {
	praxis ports.PraxisClient
}

// Execute satisfies axi-go's domain.ActionExecutor.
func (e *executor) Execute(ctx context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	in, ok := input.(map[string]any)
	if !ok {
		return domain.ExecutionResult{}, nil, fmt.Errorf("axi executor: unexpected input shape %T", input)
	}
	capability, _ := in["capability"].(string)
	idempotency, _ := in["idempotency_key"].(string)
	payloadMap, _ := in["payload"].(map[string]any)
	req := nousdomain.ActionRequest{
		Capability:     capability,
		Payload:        payloadMap,
		IdempotencyKey: idempotency,
	}
	if rawConstraints, ok := in["constraints"].([]any); ok {
		for _, c := range rawConstraints {
			if cm, ok := c.(map[string]any); ok {
				k, _ := cm["key"].(string)
				v, _ := cm["value"].(string)
				if k != "" {
					req.Constraints = append(req.Constraints, nousdomain.ActionConstraint{Key: k, Value: v})
				}
			}
		}
	}

	res, err := e.praxis.Execute(ctx, req)
	if err != nil {
		return domain.ExecutionResult{}, nil, err
	}

	evidence := []domain.EvidenceRecord{{
		Kind:   "praxis.execution",
		Source: axiPluginName,
		Value: map[string]any{
			"capability":      res.Capability,
			"success":         res.Success,
			"idempotency_key": res.IdempotencyKey,
			"executed_at":     res.ExecutedAt,
		},
	}}
	if !res.Success {
		return domain.ExecutionResult{}, evidence, fmt.Errorf("praxis: %s", res.Error)
	}
	return domain.ExecutionResult{
		Data:    res.Output,
		Summary: fmt.Sprintf("praxis ran %s", res.Capability),
	}, evidence, nil
}

// newPlugin defines a single action: nous-execute-intervention.
// The contracts are empty because validation happens at the Nous
// boundary (intervention.Validate). axi-go's effect gating is what
// we want from the kernel, not its schema validator.
func newPlugin() (axiPlugin, error) {
	name, err := domain.NewActionName(axiActionName)
	if err != nil {
		return axiPlugin{}, fmt.Errorf("invalid action name: %w", err)
	}
	action, err := domain.NewActionDefinition(
		name,
		"Forward an accepted Nous intervention to Praxis for execution.",
		domain.EmptyContract(),
		domain.EmptyContract(),
		nil,
		domain.EffectProfile{Level: domain.EffectWriteExternal},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	if err != nil {
		return axiPlugin{}, fmt.Errorf("define action: %w", err)
	}
	if err := action.BindExecutor(domain.ActionExecutorRef(axiExecutorRef)); err != nil {
		return axiPlugin{}, fmt.Errorf("bind executor: %w", err)
	}
	return axiPlugin{actions: []*domain.ActionDefinition{action}}, nil
}

type axiPlugin struct {
	actions []*domain.ActionDefinition
}

// Contribute satisfies domain.Plugin.
func (p axiPlugin) Contribute() (*domain.PluginContribution, error) {
	return domain.NewPluginContribution(axiPluginName, p.actions, nil)
}

// evidenceFrom converts axi-go's evidence chain into our kernel-
// neutral records. We rebadge anything we generated so the Mnemos
// projection has a stable shape; foreign evidence (from chained
// capabilities) flows through with its original kind preserved.
func evidenceFrom(records []domain.EvidenceRecord, _ string, _ string) []EvidenceRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]EvidenceRecord, len(records))
	for i, r := range records {
		val, _ := r.Value.(map[string]any)
		out[i] = EvidenceRecord{
			Kind:   r.Kind,
			Source: r.Source,
			Value:  val,
		}
	}
	return out
}

// axiBudgetFromEnv reads NOUS_AXI_MAX_DURATION (Go duration) and
// NOUS_AXI_MAX_INVOCATIONS (positive int). Defaults: 5 min, 1000
// invocations. Mirrors Mnemos's convention so an operator who
// already runs Mnemos finds the knobs in the same place.
func axiBudgetFromEnv() axi.Budget {
	b := axi.Budget{
		MaxDuration:              5 * time.Minute,
		MaxCapabilityInvocations: 1000,
	}
	if v := strings.TrimSpace(os.Getenv("NOUS_AXI_MAX_DURATION")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			b.MaxDuration = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("NOUS_AXI_MAX_INVOCATIONS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			b.MaxCapabilityInvocations = n
		}
	}
	return b
}

// boltAxiLogger adapts our bolt logger into axi-go's domain.Logger.
type boltAxiLogger struct{ logger *bolt.Logger }

func (l boltAxiLogger) Debug(msg string, fields ...domain.Field) { l.emit(l.logger.Debug(), msg, fields) }
func (l boltAxiLogger) Info(msg string, fields ...domain.Field)  { l.emit(l.logger.Info(), msg, fields) }
func (l boltAxiLogger) Warn(msg string, fields ...domain.Field)  { l.emit(l.logger.Warn(), msg, fields) }
func (l boltAxiLogger) Error(msg string, fields ...domain.Field) { l.emit(l.logger.Error(), msg, fields) }

func (l boltAxiLogger) emit(ev *bolt.Event, msg string, fields []domain.Field) {
	for _, f := range fields {
		ev = ev.Any(f.Key, f.Value)
	}
	ev.Msg(msg)
}

// boltAxiPublisher fans every kernel domain event into bolt as a
// structured "axi_event" line.
type boltAxiPublisher struct{ logger *bolt.Logger }

func (p boltAxiPublisher) Publish(e domain.DomainEvent) {
	p.logger.Info().
		Str("event_type", e.EventType()).
		Str("occurred_at", e.OccurredAt().UTC().Format(time.RFC3339Nano)).
		Msg("axi_event")
}
