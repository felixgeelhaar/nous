// Package grpc implements the Nous gRPC service handlers.
package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/nous/api/nous/v1"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements nousv1.NousServer.
type Server struct {
	nousv1.UnimplementedNousServer

	extractor *pipeline.Extractor
	evaluator *pipeline.Evaluator
	store     *storeRepos
}

type storeRepos struct {
	commitments   ports.CommitmentRepository
	decisions     ports.DecisionRepository
	interventions ports.InterventionRepository
}

// NewServer returns a gRPC server wired with the application pipelines.
func NewServer(extractor *pipeline.Extractor, evaluator *pipeline.Evaluator, commitments ports.CommitmentRepository, decisions ports.DecisionRepository, interventions ports.InterventionRepository) *Server {
	return &Server{
		extractor: extractor,
		evaluator: evaluator,
		store: &storeRepos{
			commitments:   commitments,
			decisions:     decisions,
			interventions: interventions,
		},
	}
}

func (s *Server) Extract(ctx context.Context, req *nousv1.ExtractRequest) (*nousv1.ExtractResponse, error) {
	if req.OwnerId == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_id is required")
	}
	if req.Text == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}

	sourceRefs := make([]domain.SourceRef, len(req.SourceRefs))
	for i, sr := range req.SourceRefs {
		sourceRefs[i] = domain.SourceRef{Kind: domain.SourceKind(sr.Kind), Locator: sr.Locator}
	}

	res, err := s.extractor.Extract(ctx, llm.ExtractInput{
		OwnerID:    req.OwnerId,
		Text:       req.Text,
		Hints:      req.Hints,
		SourceRefs: sourceRefs,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &nousv1.ExtractResponse{
		Considered: int32(res.Considered),
		SavedIds:   idsToStrings(res.Saved),
		Dropped:    int32(res.Dropped),
		DecisionId: optionalID(res.DecisionID),
	}, nil
}

func (s *Server) Evaluate(ctx context.Context, req *nousv1.EvaluateRequest) (*nousv1.EvaluateResponse, error) {
	opts := pipeline.EvaluateOptions{Limit: int(req.Limit)}
	if req.SignalLookback != nil {
		opts.SignalLookback = req.SignalLookback.AsDuration()
	}

	res, err := s.evaluator.Evaluate(ctx, opts)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &nousv1.EvaluateResponse{
		Evaluated:        int32(res.Evaluated),
		Updated:          int32(res.Updated),
		InterventionIds:  idsToStrings(res.Interventions),
		DecisionIds:      idsToStrings(res.Decisions),
	}, nil
}

func (s *Server) ListCommitments(ctx context.Context, req *nousv1.ListCommitmentsRequest) (*nousv1.ListCommitmentsResponse, error) {
	statuses := make([]domain.CommitmentStatus, len(req.Statuses))
	for i, s := range req.Statuses {
		statuses[i] = domain.CommitmentStatus(s)
	}
	var dueBy *time.Time
	if req.DueBy != nil {
		t := req.DueBy.AsTime()
		dueBy = &t
	}

	list, err := s.store.commitments.List(ctx, ports.CommitmentFilter{
		OwnerID:  req.OwnerId,
		Statuses: statuses,
		DueBy:    dueBy,
		Limit:    int(req.Limit),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	out := make([]*nousv1.Commitment, len(list))
	for i, c := range list {
		out[i] = commitmentToProto(c)
	}
	return &nousv1.ListCommitmentsResponse{Commitments: out}, nil
}

func (s *Server) GetCommitment(ctx context.Context, req *nousv1.GetCommitmentRequest) (*nousv1.Commitment, error) {
	id, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	c, err := s.store.commitments.Get(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrCommitmentNotFound) {
			return nil, status.Error(codes.NotFound, "commitment not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return commitmentToProto(c), nil
}

func (s *Server) ListDecisions(ctx context.Context, req *nousv1.ListDecisionsRequest) (*nousv1.ListDecisionsResponse, error) {
	list, err := s.store.decisions.ListBySubject(ctx, req.Subject, int(req.Limit))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*nousv1.Decision, len(list))
	for i, d := range list {
		out[i] = decisionToProto(d)
	}
	return &nousv1.ListDecisionsResponse{Decisions: out}, nil
}

func (s *Server) GetDecision(ctx context.Context, req *nousv1.GetDecisionRequest) (*nousv1.Decision, error) {
	id, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	d, err := s.store.decisions.Get(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrDecisionNotFound) {
			return nil, status.Error(codes.NotFound, "decision not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return decisionToProto(d), nil
}

func (s *Server) ListInterventions(ctx context.Context, req *nousv1.ListInterventionsRequest) (*nousv1.ListInterventionsResponse, error) {
	statuses := make([]domain.InterventionStatus, len(req.Statuses))
	for i, st := range req.Statuses {
		statuses[i] = domain.InterventionStatus(st)
	}
	var since *time.Time
	if req.Since != nil {
		t := req.Since.AsTime()
		since = &t
	}
	var commitmentID *domain.CommitmentID
	if req.CommitmentId != "" {
		id, err := uuid.Parse(req.CommitmentId)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid commitment_id")
		}
		commitmentID = &id
	}
	var taskID *domain.TaskID
	if req.TaskId != "" {
		id, err := uuid.Parse(req.TaskId)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid task_id")
		}
		taskID = &id
	}

	list, err := s.store.interventions.List(ctx, ports.InterventionFilter{
		CommitmentID: commitmentID,
		TaskID:       taskID,
		Statuses:     statuses,
		Since:        since,
		Limit:        int(req.Limit),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*nousv1.Intervention, len(list))
	for i, iv := range list {
		out[i] = interventionToProto(iv)
	}
	return &nousv1.ListInterventionsResponse{Interventions: out}, nil
}

func (s *Server) GetIntervention(ctx context.Context, req *nousv1.GetInterventionRequest) (*nousv1.Intervention, error) {
	id, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	iv, err := s.store.interventions.Get(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrInterventionNotFound) {
			return nil, status.Error(codes.NotFound, "intervention not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return interventionToProto(iv), nil
}

func (s *Server) ResolveIntervention(ctx context.Context, req *nousv1.ResolveInterventionRequest) (*nousv1.Intervention, error) {
	id, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	iv, err := s.store.interventions.Get(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrInterventionNotFound) {
			return nil, status.Error(codes.NotFound, "intervention not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	ivStatus := domain.InterventionStatus(req.Status)
	if !ivStatus.Valid() {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid status %q", req.Status))
	}
	if err := iv.Resolve(ivStatus, time.Now().UTC()); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	if err := s.store.interventions.Save(ctx, iv); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return interventionToProto(iv), nil
}

// --- mappers ---

func commitmentToProto(c domain.Commitment) *nousv1.Commitment {
	out := &nousv1.Commitment{
		Id:          c.ID.String(),
		OwnerId:     c.OwnerID,
		Description: c.Description,
		Status:      string(c.Status),
		Confidence:  c.Confidence,
		RiskScore:   c.RiskScore,
		CreatedAt:   timestamppb.New(c.CreatedAt),
		UpdatedAt:   timestamppb.New(c.UpdatedAt),
	}
	if c.DueAt != nil {
		out.DueAt = timestamppb.New(*c.DueAt)
	}
	if c.LastEvaluatedAt != nil {
		out.LastEvaluatedAt = timestamppb.New(*c.LastEvaluatedAt)
	}
	out.SourceRefs = make([]*nousv1.SourceRef, len(c.SourceRefs))
	for i, sr := range c.SourceRefs {
		out.SourceRefs[i] = &nousv1.SourceRef{Kind: string(sr.Kind), Locator: sr.Locator}
	}
	out.Entities = make([]*nousv1.EntityRef, len(c.Entities))
	for i, e := range c.Entities {
		out.Entities[i] = &nousv1.EntityRef{Kind: string(e.Kind), Name: e.Name, Id: e.ID}
	}
	return out
}

func decisionToProto(d domain.Decision) *nousv1.Decision {
	out := &nousv1.Decision{
		Id:         d.ID.String(),
		Subject:    d.Subject,
		Reason:     d.Reason,
		Confidence: d.Confidence,
		CreatedAt:  timestamppb.New(d.CreatedAt),
	}
	out.ContextRefs = make([]*nousv1.SourceRef, len(d.ContextRefs))
	for i, sr := range d.ContextRefs {
		out.ContextRefs[i] = &nousv1.SourceRef{Kind: string(sr.Kind), Locator: sr.Locator}
	}
	out.Inputs = stringifyMap(d.Inputs)
	out.Outcome = stringifyMap(d.Outcome)
	return out
}

func interventionToProto(iv domain.Intervention) *nousv1.Intervention {
	out := &nousv1.Intervention{
		Id:          iv.ID.String(),
		Type:        string(iv.Type),
		Message:     iv.Message,
		Status:      string(iv.Status),
		TriggeredAt: timestamppb.New(iv.TriggeredAt),
	}
	if iv.CommitmentID != nil {
		out.CommitmentId = iv.CommitmentID.String()
	}
	if iv.TaskID != nil {
		out.TaskId = iv.TaskID.String()
	}
	if iv.SuggestedAction != nil {
		out.SuggestedAction = actionRequestToProto(*iv.SuggestedAction)
	}
	if iv.ResolvedAt != nil {
		out.ResolvedAt = timestamppb.New(*iv.ResolvedAt)
	}
	return out
}

func actionRequestToProto(ar domain.ActionRequest) *nousv1.ActionRequest {
	out := &nousv1.ActionRequest{
		Id:             ar.ID.String(),
		Capability:     ar.Capability,
		IdempotencyKey: ar.IdempotencyKey,
	}
	if ar.Payload != nil {
		out.Payload = stringifyAnyMap(ar.Payload)
	}
	out.Constraints = make([]*nousv1.ActionConstraint, len(ar.Constraints))
	for i, c := range ar.Constraints {
		out.Constraints[i] = &nousv1.ActionConstraint{Key: c.Key, Value: c.Value}
	}
	return out
}

func stringifyMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func stringifyAnyMap(m map[string]any) map[string]string {
	return stringifyMap(m)
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
