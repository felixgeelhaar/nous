package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/felixgeelhaar/nous/internal/domain"
)

// BedrockProvider implements Provider using Amazon Bedrock's Converse
// API. The Converse API normalises the request/response shape across
// every model Bedrock hosts (Anthropic, Meta, Cohere, ...), so this
// single provider works for any Bedrock-hosted model. AWS SigV4
// signing is handled by the AWS SDK; credentials come from the
// standard AWS credential chain (env vars, shared config, EC2/ECS/EKS
// instance profile, IRSA).
type BedrockProvider struct {
	client *bedrockruntime.Client
	model  string
}

// BedrockOption tunes a provider after construction.
type BedrockOption func(*BedrockProvider)

// WithBedrockClient overrides the underlying Bedrock client. Tests
// inject mocks via this seam.
func WithBedrockClient(c *bedrockruntime.Client) BedrockOption {
	return func(p *BedrockProvider) { p.client = c }
}

// NewBedrockProvider creates a new Bedrock LLM provider. The region
// override falls back to AWS_REGION / AWS_DEFAULT_REGION. modelID is
// the Bedrock model identifier (e.g.
// "anthropic.claude-opus-4-7-20260201-v1:0",
// "meta.llama3-70b-instruct-v1:0", ...). Returns an error when AWS
// credentials cannot be loaded.
func NewBedrockProvider(ctx context.Context, region, modelID string, opts ...BedrockOption) (*BedrockProvider, error) {
	if modelID == "" {
		modelID = "anthropic.claude-opus-4-7-20260201-v1:0"
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("bedrock: load aws config: %w", err)
	}
	p := &BedrockProvider{
		client: bedrockruntime.NewFromConfig(cfg),
		model:  modelID,
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// ExtractCommitments calls Bedrock's InvokeModel with an
// Anthropic-flavoured payload and parses the JSON reply. The
// Anthropic-on-Bedrock body shape mirrors the Messages API.
func (p *BedrockProvider) ExtractCommitments(ctx context.Context, prompt string) ([]domain.CommitmentDraft, error) {
	if p.client == nil {
		return nil, errors.New("bedrock: client not initialised")
	}
	body, err := json.Marshal(map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock: marshal request: %w", err)
	}
	out, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(p.model),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock: invoke: %w", err)
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out.Body, &parsed); err != nil {
		return nil, fmt.Errorf("bedrock: decode response: %w", err)
	}
	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	if text == "" {
		return nil, nil
	}
	return parseDrafts(text)
}
