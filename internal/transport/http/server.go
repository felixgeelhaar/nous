// Package http exposes a lightweight HTTP surface over the Nous gRPC
// service. It is not a full gRPC-Gateway replacement; instead it
// provides REST-shaped endpoints that proxy to the internal service
// methods.
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/observability"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/validation"
	"github.com/google/uuid"
)

// Server is a minimal HTTP adapter over the Nous application layer.
type Server struct {
	mux           *http.ServeMux
	extractor     *pipeline.Extractor
	evaluator     *pipeline.Evaluator
	commitments   ports.CommitmentRepository
	decisions     ports.DecisionRepository
	intervs       ports.InterventionRepository
	metrics       *observability.Metrics
	healthChecker func(context.Context) error
	validator     *validation.Validator
}

// WithValidator attaches a CEL validator to the Server. Nil disables
// validation. Returns the same server for chaining.
func (s *Server) WithValidator(v *validation.Validator) *Server {
	s.validator = v
	return s
}

// NewServer returns an HTTP server wired with the application layer.
func NewServer(extractor *pipeline.Extractor, evaluator *pipeline.Evaluator, commitments ports.CommitmentRepository, decisions ports.DecisionRepository, interventions ports.InterventionRepository, metrics *observability.Metrics, healthChecker func(context.Context) error) *Server {
	s := &Server{
		mux:           http.NewServeMux(),
		extractor:     extractor,
		evaluator:     evaluator,
		commitments:   commitments,
		decisions:     decisions,
		intervs:       interventions,
		metrics:       metrics,
		healthChecker: healthChecker,
	}
	s.mux.HandleFunc("POST /v1/extract", s.handleExtract)
	s.mux.HandleFunc("POST /v1/evaluate", s.handleEvaluate)
	s.mux.HandleFunc("GET /v1/commitments", s.handleListCommitments)
	s.mux.HandleFunc("GET /v1/commitments/{id}", s.handleGetCommitment)
	s.mux.HandleFunc("GET /v1/decisions", s.handleListDecisions)
	s.mux.HandleFunc("GET /v1/decisions/{id}", s.handleGetDecision)
	s.mux.HandleFunc("GET /v1/interventions", s.handleListInterventions)
	s.mux.HandleFunc("POST /v1/interventions/{id}/resolve", s.handleResolveIntervention)
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	return s
}

// Handler returns the root http.Handler for mounting.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	var req extractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.OwnerID == "" || req.Text == "" {
		httpError(w, http.StatusBadRequest, "owner_id and text are required")
		return
	}

	sourceRefs := make([]domain.SourceRef, len(req.SourceRefs))
	for i, sr := range req.SourceRefs {
		sourceRefs[i] = domain.SourceRef{Kind: domain.SourceKind(sr.Kind), Locator: sr.Locator}
	}

	if s.validator != nil {
		kinds := make([]string, len(req.SourceRefs))
		for i, sr := range req.SourceRefs {
			kinds[i] = sr.Kind
		}
		if err := s.validator.Validate(validation.Input{
			OwnerID:     req.OwnerID,
			Text:        req.Text,
			SourceKinds: kinds,
		}); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	res, err := s.extractor.Extract(r.Context(), llm.ExtractInput{
		OwnerID:    req.OwnerID,
		Text:       req.Text,
		Hints:      req.Hints,
		SourceRefs: sourceRefs,
	})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respond(w, extractResponse{
		Considered: res.Considered,
		SavedIDs:   idsToStrings(res.Saved),
		Dropped:    res.Dropped,
		DecisionID: optionalID(res.DecisionID),
	})
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	res, err := s.evaluator.Evaluate(r.Context(), pipeline.EvaluateOptions{})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, evaluateResponse{
		Evaluated:       res.Evaluated,
		Updated:         res.Updated,
		InterventionIDs: idsToStrings(res.Interventions),
		DecisionIDs:     idsToStrings(res.Decisions),
	})
}

func (s *Server) handleListCommitments(w http.ResponseWriter, r *http.Request) {
	ownerID := r.URL.Query().Get("owner_id")
	list, err := s.commitments.List(r.Context(), ports.CommitmentFilter{OwnerID: ownerID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]commitmentJSON, len(list))
	for i, c := range list {
		out[i] = commitmentToJSON(c)
	}
	respond(w, out)
}

func (s *Server) handleGetCommitment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.commitments.Get(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "commitment not found")
		return
	}
	respond(w, commitmentToJSON(c))
}

func (s *Server) handleListDecisions(w http.ResponseWriter, r *http.Request) {
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		httpError(w, http.StatusBadRequest, "subject is required")
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, err := s.decisions.ListBySubject(r.Context(), subject, limit)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]decisionJSON, len(list))
	for i, d := range list {
		out[i] = decisionToJSON(d)
	}
	respond(w, out)
}

func (s *Server) handleGetDecision(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := s.decisions.Get(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "decision not found")
		return
	}
	respond(w, decisionToJSON(d))
}

func (s *Server) handleListInterventions(w http.ResponseWriter, r *http.Request) {
	commitmentIDStr := r.URL.Query().Get("commitment_id")
	var commitmentID *domain.CommitmentID
	if commitmentIDStr != "" {
		id, err := uuid.Parse(commitmentIDStr)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid commitment_id")
			return
		}
		commitmentID = &id
	}

	list, err := s.intervs.List(r.Context(), ports.InterventionFilter{CommitmentID: commitmentID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]interventionJSON, len(list))
	for i, iv := range list {
		out[i] = interventionToJSON(iv)
	}
	respond(w, out)
}

func (s *Server) handleResolveIntervention(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	iv, err := s.intervs.Get(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "intervention not found")
		return
	}

	status := domain.InterventionStatus(req.Status)
	if !status.Valid() {
		httpError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := iv.Resolve(status, time.Now().UTC()); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.intervs.Save(r.Context(), iv); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, interventionToJSON(iv))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]any{"status": "healthy"}
	if s.healthChecker != nil {
		if err := s.healthChecker(r.Context()); err != nil {
			status = http.StatusServiceUnavailable
			body["status"] = "unhealthy"
			body["error"] = err.Error()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metrics != nil {
		s.metrics.Handler().ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("# Metrics endpoint not configured\n"))
}

// --- request/response DTOs ---

type extractRequest struct {
	OwnerID    string      `json:"owner_id"`
	Text       string      `json:"text"`
	Hints      []string    `json:"hints,omitempty"`
	SourceRefs []sourceRef `json:"source_refs,omitempty"`
}

type extractResponse struct {
	Considered int      `json:"considered"`
	SavedIDs   []string `json:"saved_ids"`
	Dropped    int      `json:"dropped"`
	DecisionID string   `json:"decision_id,omitempty"`
}

type evaluateResponse struct {
	Evaluated       int      `json:"evaluated"`
	Updated         int      `json:"updated"`
	InterventionIDs []string `json:"intervention_ids"`
	DecisionIDs     []string `json:"decision_ids"`
}

type resolveRequest struct {
	Status string `json:"status"`
}

type sourceRef struct {
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
}

type commitmentJSON struct {
	ID          string     `json:"id"`
	OwnerID     string     `json:"owner_id"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Confidence  float64    `json:"confidence"`
	RiskScore   float64    `json:"risk_score"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type decisionJSON struct {
	ID          string             `json:"id"`
	Subject     string             `json:"subject"`
	ContextRefs []sourceRef        `json:"context_refs,omitempty"`
	Inputs      map[string]any     `json:"inputs,omitempty"`
	Weights     map[string]float64 `json:"weights,omitempty"`
	Outcome     map[string]any     `json:"outcome,omitempty"`
	Reason      string             `json:"reason"`
	Confidence  float64            `json:"confidence"`
	CreatedAt   time.Time          `json:"created_at"`
}

type interventionJSON struct {
	ID          string     `json:"id"`
	CommitmentID string    `json:"commitment_id,omitempty"`
	Type        string     `json:"type"`
	Message     string     `json:"message"`
	Status      string     `json:"status"`
	TriggeredAt time.Time  `json:"triggered_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

func commitmentToJSON(c domain.Commitment) commitmentJSON {
	out := commitmentJSON{
		ID:          c.ID.String(),
		OwnerID:     c.OwnerID,
		Description: c.Description,
		Status:      string(c.Status),
		Confidence:  c.Confidence,
		RiskScore:   c.RiskScore,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
	if c.DueAt != nil {
		out.DueAt = c.DueAt
	}
	return out
}

func decisionToJSON(d domain.Decision) decisionJSON {
	out := decisionJSON{
		ID:         d.ID.String(),
		Subject:    d.Subject,
		Inputs:     map[string]any(d.Inputs),
		Weights:    map[string]float64(d.Weights),
		Outcome:    map[string]any(d.Outcome),
		Reason:     d.Reason,
		Confidence: d.Confidence,
		CreatedAt:  d.CreatedAt,
	}
	if len(d.ContextRefs) > 0 {
		out.ContextRefs = make([]sourceRef, len(d.ContextRefs))
		for i, ref := range d.ContextRefs {
			out.ContextRefs[i] = sourceRef{Kind: string(ref.Kind), Locator: ref.Locator}
		}
	}
	return out
}

func interventionToJSON(iv domain.Intervention) interventionJSON {
	out := interventionJSON{
		ID:          iv.ID.String(),
		Type:        string(iv.Type),
		Message:     iv.Message,
		Status:      string(iv.Status),
		TriggeredAt: iv.TriggeredAt,
	}
	if iv.CommitmentID != nil {
		out.CommitmentID = iv.CommitmentID.String()
	}
	if iv.ResolvedAt != nil {
		out.ResolvedAt = iv.ResolvedAt
	}
	return out
}

func idsToStrings[T ~[16]byte](ids []T) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = uuid.UUID(id).String()
	}
	return out
}

func optionalID[T ~[16]byte](id *T) string {
	if id == nil {
		return ""
	}
	return uuid.UUID(*id).String()
}

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
