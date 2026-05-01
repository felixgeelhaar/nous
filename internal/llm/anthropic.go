package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// AnthropicProvider implements Provider using Anthropic's Messages API.
type AnthropicProvider struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
}

// AnthropicOption tunes a provider after construction.
type AnthropicOption func(*AnthropicProvider)

// WithAnthropicHTTPClient overrides the default HTTP client. Tests
// inject httptest servers via this seam.
func WithAnthropicHTTPClient(c *http.Client) AnthropicOption {
	return func(p *AnthropicProvider) { p.client = c }
}

// WithAnthropicBaseURL points the provider at a non-default endpoint.
// Production deployments leave this empty; tests point at httptest.
func WithAnthropicBaseURL(u string) AnthropicOption {
	return func(p *AnthropicProvider) { p.baseURL = u }
}

// NewAnthropicProvider creates a new Anthropic LLM provider.
func NewAnthropicProvider(apiKey, model string, opts ...AnthropicOption) *AnthropicProvider {
	if model == "" {
		model = "claude-opus-4-7"
	}
	p := &AnthropicProvider{
		apiKey:  apiKey,
		model:   model,
		client:  defaultHTTPClient(),
		baseURL: "https://api.anthropic.com",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// anthropicMessagesRequest is the subset of the Messages API request
// schema we use for commitment extraction.
type anthropicMessagesRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessagesResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Error      *anthropicError         `json:"error,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ExtractCommitments calls Claude's Messages API and parses the JSON
// reply. Errors are wrapped so callers can branch on transport vs
// parse vs API failures.
func (p *AnthropicProvider) ExtractCommitments(ctx context.Context, prompt string) ([]domain.CommitmentDraft, error) {
	if p.apiKey == "" {
		return nil, errors.New("anthropic: api key required")
	}

	body, err := json.Marshal(anthropicMessagesRequest{
		Model:     p.model,
		MaxTokens: 1024,
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr anthropicMessagesResponse
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error != nil {
			return nil, fmt.Errorf("anthropic: %d %s: %s", resp.StatusCode, apiErr.Error.Type, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("anthropic: %d: %s", resp.StatusCode, string(raw))
	}

	var parsed anthropicMessagesResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
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
