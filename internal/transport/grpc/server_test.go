package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	nousv1 "github.com/felixgeelhaar/nous/api/nous/v1"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func newTestServer(t *testing.T) (*grpc.Server, nousv1.NousClient, func()) {
	ctx := t.Context()
	store := memory.New()

	extractor, err := pipeline.NewExtractor(pipeline.ExtractorConfig{
		Commitments:   store.Commitments,
		Decisions:     store.Decisions,
		Extractor:     llm.NewScriptedExtractor(),
		MinConfidence: 0.0,
	})
	require.NoError(t, err)

	eval, err := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   store.Commitments,
		Decisions:     store.Decisions,
		Interventions: store.Interventions,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
	})
	require.NoError(t, err)

	// Seed a commitment so evaluate has work
	now := time.Now().UTC()
	due := now.Add(-time.Hour) // overdue
	c, err := domain.NewCommitment("grpc-test", "ship the release", &due, 0.9, now)
	require.NoError(t, err)
	require.NoError(t, store.Commitments.Save(ctx, c))

	grpcSrv := grpc.NewServer()
	srv := NewServer(extractor, eval, store.Commitments, store.Decisions, store.Interventions)
	nousv1.RegisterNousServer(grpcSrv, srv)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = grpcSrv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	client := nousv1.NewNousClient(conn)

	cleanup := func() {
		conn.Close()
		grpcSrv.Stop()
		lis.Close()
	}

	return grpcSrv, client, cleanup
}

func TestGRPC_Extract(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	res, err := client.Extract(context.Background(), &nousv1.ExtractRequest{
		OwnerId: "alice",
		Text:    "I will call the client tomorrow and finish the report by Friday.",
		Hints:   []string{"work"},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, res.Considered, int32(0))
	require.Equal(t, int32(len(res.SavedIds))+res.Dropped, res.Considered)
}

func TestGRPC_ExtractValidation(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.Extract(context.Background(), &nousv1.ExtractRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGRPC_Evaluate(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	res, err := client.Evaluate(context.Background(), &nousv1.EvaluateRequest{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, res.Evaluated, int32(0))
}

func TestGRPC_EvaluateWithLimit(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	res, err := client.Evaluate(context.Background(), &nousv1.EvaluateRequest{
		Limit:          5,
		SignalLookback: durationpb.New(24 * time.Hour),
	})
	require.NoError(t, err)
	require.LessOrEqual(t, res.Evaluated, int32(5))
}

func TestGRPC_ListCommitments(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	res, err := client.ListCommitments(context.Background(), &nousv1.ListCommitmentsRequest{
		OwnerId: "grpc-test",
	})
	require.NoError(t, err)
	require.Len(t, res.Commitments, 1)
	require.Equal(t, "grpc-test", res.Commitments[0].OwnerId)
}

func TestGRPC_GetCommitment(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	// First list to get an ID
	listRes, err := client.ListCommitments(context.Background(), &nousv1.ListCommitmentsRequest{OwnerId: "grpc-test"})
	require.NoError(t, err)
	require.Len(t, listRes.Commitments, 1)
	id := listRes.Commitments[0].Id

	res, err := client.GetCommitment(context.Background(), &nousv1.GetCommitmentRequest{Id: id})
	require.NoError(t, err)
	require.Equal(t, id, res.Id)
}

func TestGRPC_GetCommitment_NotFound(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.GetCommitment(context.Background(), &nousv1.GetCommitmentRequest{
		Id: uuid.Must(uuid.NewRandom()).String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGRPC_GetCommitment_InvalidID(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.GetCommitment(context.Background(), &nousv1.GetCommitmentRequest{Id: "not-a-uuid"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGRPC_ListDecisions(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	res, err := client.ListDecisions(context.Background(), &nousv1.ListDecisionsRequest{
		Subject: "commitment.extract",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestGRPC_GetDecision(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	// List decisions first
	listRes, err := client.ListDecisions(context.Background(), &nousv1.ListDecisionsRequest{Subject: "commitment.extract"})
	require.NoError(t, err)
	if len(listRes.Decisions) == 0 {
		t.Skip("no decisions to test with")
	}

	res, err := client.GetDecision(context.Background(), &nousv1.GetDecisionRequest{Id: listRes.Decisions[0].Id})
	require.NoError(t, err)
	require.Equal(t, listRes.Decisions[0].Id, res.Id)
}

func TestGRPC_GetDecision_NotFound(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.GetDecision(context.Background(), &nousv1.GetDecisionRequest{
		Id: uuid.Must(uuid.NewRandom()).String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGRPC_ListInterventions(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	// Run evaluate to create interventions
	_, err := client.Evaluate(context.Background(), &nousv1.EvaluateRequest{})
	require.NoError(t, err)

	res, err := client.ListInterventions(context.Background(), &nousv1.ListInterventionsRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, res.Interventions)
}

func TestGRPC_GetIntervention(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	// Run evaluate to create interventions
	_, err := client.Evaluate(context.Background(), &nousv1.EvaluateRequest{})
	require.NoError(t, err)

	listRes, err := client.ListInterventions(context.Background(), &nousv1.ListInterventionsRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, listRes.Interventions)

	res, err := client.GetIntervention(context.Background(), &nousv1.GetInterventionRequest{
		Id: listRes.Interventions[0].Id,
	})
	require.NoError(t, err)
	require.Equal(t, listRes.Interventions[0].Id, res.Id)
}

func TestGRPC_GetIntervention_NotFound(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.GetIntervention(context.Background(), &nousv1.GetInterventionRequest{
		Id: uuid.Must(uuid.NewRandom()).String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGRPC_ResolveIntervention(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	// Run evaluate to create interventions
	_, err := client.Evaluate(context.Background(), &nousv1.EvaluateRequest{})
	require.NoError(t, err)

	listRes, err := client.ListInterventions(context.Background(), &nousv1.ListInterventionsRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, listRes.Interventions)

	res, err := client.ResolveIntervention(context.Background(), &nousv1.ResolveInterventionRequest{
		Id:     listRes.Interventions[0].Id,
		Status: "accepted",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", res.Status)
	require.NotNil(t, res.ResolvedAt)
}

func TestGRPC_ResolveIntervention_InvalidStatus(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	// Run evaluate to create interventions
	_, err := client.Evaluate(context.Background(), &nousv1.EvaluateRequest{})
	require.NoError(t, err)

	listRes, err := client.ListInterventions(context.Background(), &nousv1.ListInterventionsRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, listRes.Interventions)

	_, err = client.ResolveIntervention(context.Background(), &nousv1.ResolveInterventionRequest{
		Id:     listRes.Interventions[0].Id,
		Status: "garbage",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGRPC_ResolveIntervention_NotFound(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.ResolveIntervention(context.Background(), &nousv1.ResolveInterventionRequest{
		Id:     uuid.Must(uuid.NewRandom()).String(),
		Status: "accepted",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGRPC_ResolveIntervention_InvalidID(t *testing.T) {
	_, client, cleanup := newTestServer(t)
	defer cleanup()

	_, err := client.ResolveIntervention(context.Background(), &nousv1.ResolveInterventionRequest{
		Id:     "bad-id",
		Status: "accepted",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

// --- mapper tests ---

func TestCommitmentToProto(t *testing.T) {
	now := time.Now().UTC()
	due := now.Add(time.Hour)
	c, err := domain.NewCommitment("o", "desc", &due, 0.8, now)
	require.NoError(t, err)
	c.Status = domain.CommitmentCompleted
	c.RiskScore = 0.5
	c.LastEvaluatedAt = &now
	c.SourceRefs = []domain.SourceRef{{Kind: domain.SourceUserInput, Locator: "test@example.com"}}
	c.Entities = []domain.EntityRef{{Kind: domain.EntityPerson, Name: "Alice", ID: "alice-id"}}

	p := commitmentToProto(c)
	require.Equal(t, c.ID.String(), p.Id)
	require.Equal(t, "o", p.OwnerId)
	require.Equal(t, "desc", p.Description)
	require.Equal(t, "completed", p.Status)
	require.InDelta(t, 0.8, p.Confidence, 0.001)
	require.InDelta(t, 0.5, p.RiskScore, 0.001)
	require.NotNil(t, p.DueAt)
	require.NotNil(t, p.LastEvaluatedAt)
	require.Len(t, p.SourceRefs, 1)
	require.Len(t, p.Entities, 1)
}

func TestDecisionToProto(t *testing.T) {
	now := time.Now().UTC()
	d, err := domain.NewDecision("test.decision", "reason", 0.9, now)
	require.NoError(t, err)
	d.ContextRefs = []domain.SourceRef{{Kind: domain.SourceMnemosMemory, Locator: "msg-123"}}
	d.Inputs = domain.DecisionInputs{"key": "value"}
	d.Outcome = domain.DecisionOutcome{"result": "ok"}

	p := decisionToProto(d)
	require.Equal(t, d.ID.String(), p.Id)
	require.Equal(t, "test.decision", p.Subject)
	require.Equal(t, "reason", p.Reason)
	require.InDelta(t, 0.9, p.Confidence, 0.001)
	require.NotNil(t, p.CreatedAt)
	require.Len(t, p.ContextRefs, 1)
	require.Equal(t, "value", p.Inputs["key"])
	require.Equal(t, "ok", p.Outcome["result"])
}

func TestInterventionToProto(t *testing.T) {
	now := time.Now().UTC()
	cid := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	iv, err := domain.NewIntervention(domain.InterventionNudge, "msg", &cid, nil, nil, now)
	require.NoError(t, err)
	iv.Status = domain.InterventionExecuted
	iv.ResolvedAt = &now

	p := interventionToProto(iv)
	require.Equal(t, iv.ID.String(), p.Id)
	require.Equal(t, "nudge", p.Type)
	require.Equal(t, "msg", p.Message)
	require.Equal(t, "executed", p.Status)
	require.Equal(t, cid.String(), p.CommitmentId)
	require.NotNil(t, p.TriggeredAt)
	require.NotNil(t, p.ResolvedAt)
}

func TestInterventionToProto_WithAction(t *testing.T) {
	now := time.Now().UTC()
	cid := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	action := &domain.ActionRequest{
		ID:             uuid.New(),
		Capability:     "send_email",
		IdempotencyKey: "key-123",
		Payload:        map[string]any{"to": "alice@example.com"},
		Constraints:    []domain.ActionConstraint{{Key: "timeout", Value: "30s"}},
	}
	iv, err := domain.NewIntervention(domain.InterventionAutomation, "auto", &cid, nil, action, now)
	require.NoError(t, err)

	p := interventionToProto(iv)
	require.NotNil(t, p.SuggestedAction)
	require.Equal(t, "send_email", p.SuggestedAction.Capability)
	require.Equal(t, "key-123", p.SuggestedAction.IdempotencyKey)
	require.Equal(t, "alice@example.com", p.SuggestedAction.Payload["to"])
	require.Len(t, p.SuggestedAction.Constraints, 1)
}

func TestActionRequestToProto(t *testing.T) {
	ar := domain.ActionRequest{
		ID:             uuid.New(),
		Capability:     "test",
		IdempotencyKey: "key",
		Payload:        map[string]any{"num": 42, "bool": true},
		Constraints:    []domain.ActionConstraint{},
	}
	p := actionRequestToProto(ar)
	require.Equal(t, ar.ID.String(), p.Id)
	require.Equal(t, "42", p.Payload["num"])
	require.Equal(t, "true", p.Payload["bool"])
}

func TestIdsToStrings(t *testing.T) {
	ids := []uuid.UUID{
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	}
	out := idsToStrings(ids)
	require.Equal(t, []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	}, out)
}

func TestOptionalID(t *testing.T) {
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	require.Equal(t, "33333333-3333-3333-3333-333333333333", optionalID(&id))
	var nilID *uuid.UUID
	require.Equal(t, "", optionalID(nilID))
}
