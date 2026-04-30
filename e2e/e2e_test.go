package e2e_test

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
	grpcserver "github.com/felixgeelhaar/nous/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestE2E_ExtractAndEvaluate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-30 * time.Minute)

	conn := memory.New()

	riskEngine := risk.New(risk.DefaultConfig())
	ivEngine := intervention.New(intervention.DefaultConfig())

	scripted := llm.NewScriptedExtractor(llm.ScriptRule{
		Trigger: llm.ContainsAny("follow up"),
		Drafts: []domain.CommitmentDraft{
			{Description: "follow up with alex", DueAt: &overdue, Confidence: 0.92},
		},
	})
	extractor, err := pipeline.NewExtractor(pipeline.ExtractorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Extractor:     scripted,
		MinConfidence: 0.5,
		Clock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}

	evaluator, err := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Risk:          riskEngine,
		Intervention:  ivEngine,
		Clock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("evaluator: %v", err)
	}

	grpcSrv := grpc.NewServer()
	nousv1.RegisterNousServer(grpcSrv, grpcserver.NewServer(
		extractor, evaluator,
		conn.Commitments, conn.Decisions, conn.Interventions,
	))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			t.Logf("gRPC server stopped: %v", err)
		}
	}()
	defer grpcSrv.GracefulStop()

	clientConn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = clientConn.Close() }()
	client := nousv1.NewNousClient(clientConn)

	// Extract
	extractRes, err := client.Extract(ctx, &nousv1.ExtractRequest{
		OwnerId: "u1",
		Text:    "I'll follow up with Alex",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if extractRes.Considered != 1 || len(extractRes.SavedIds) != 1 {
		t.Fatalf("expected 1 saved, got %+v", extractRes)
	}
	commitmentID := extractRes.SavedIds[0]

	// Evaluate
	evalRes, err := client.Evaluate(ctx, &nousv1.EvaluateRequest{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if evalRes.Evaluated < 1 {
		t.Fatalf("expected >=1 evaluated, got %d", evalRes.Evaluated)
	}

	// List interventions
	listRes, err := client.ListInterventions(ctx, &nousv1.ListInterventionsRequest{})
	if err != nil {
		t.Fatalf("list interventions: %v", err)
	}
	if len(listRes.Interventions) < 1 {
		t.Fatalf("expected >=1 intervention, got %d", len(listRes.Interventions))
	}
	iv := listRes.Interventions[0]
	if iv.CommitmentId != commitmentID {
		t.Errorf("intervention commitment_id = %q, want %q", iv.CommitmentId, commitmentID)
	}

	// Resolve intervention
	resolveRes, err := client.ResolveIntervention(ctx, &nousv1.ResolveInterventionRequest{
		Id:     iv.Id,
		Status: "accepted",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolveRes.Status != "accepted" {
		t.Errorf("resolved status = %q, want accepted", resolveRes.Status)
	}

	// Get commitment
	commitRes, err := client.GetCommitment(ctx, &nousv1.GetCommitmentRequest{Id: commitmentID})
	if err != nil {
		t.Fatalf("get commitment: %v", err)
	}
	if commitRes.RiskScore <= 0 {
		t.Errorf("expected risk score >0 after evaluate, got %v", commitRes.RiskScore)
	}
}
