package llm

import (
	"context"
	"os"
	"testing"
)

func TestBedrockProvider_RejectsUninitialisedClient(t *testing.T) {
	t.Parallel()
	p := &BedrockProvider{model: "anthropic.test"}
	_, err := p.ExtractCommitments(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
}

// Live Bedrock calls require valid AWS credentials and incur cost,
// so the integration test is gated on an env var. CI does not set
// it; operators run it manually.
//
// Run: AWS_REGION=us-east-1 NOUS_BEDROCK_INTEGRATION=1 go test ./internal/llm -run TestBedrockProvider_LiveExtract
func TestBedrockProvider_LiveExtract(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if v := os.Getenv("NOUS_BEDROCK_INTEGRATION"); v == "" {
		t.Skip("NOUS_BEDROCK_INTEGRATION not set")
	}
	p, err := NewBedrockProvider(context.Background(), os.Getenv("AWS_REGION"), "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	drafts, err := p.ExtractCommitments(context.Background(), `Extract commitments from: "I'll send the report by Friday." Return JSON array.`)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) == 0 {
		t.Errorf("zero drafts; want ≥ 1")
	}
}
