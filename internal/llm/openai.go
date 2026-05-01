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

// OpenAIProvider implements Provider using OpenAI's Chat Completions API.
type OpenAIProvider struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
}

// OpenAIOption tunes a provider after construction.
type OpenAIOption func(*OpenAIProvider)

// WithOpenAIHTTPClient overrides the default HTTP client.
func WithOpenAIHTTPClient(c *http.Client) OpenAIOption {
	return func(p *OpenAIProvider) { p.client = c }
}

// WithOpenAIBaseURL points the provider at a non-default endpoint.
func WithOpenAIBaseURL(u string) OpenAIOption {
	return func(p *OpenAIProvider) { p.baseURL = u }
}

// NewOpenAIProvider creates a new OpenAI LLM provider.
func NewOpenAIProvider(apiKey, model string, opts ...OpenAIOption) *OpenAIProvider {
	if model == "" {
		model = "gpt-4o-mini"
	}
	p := &OpenAIProvider{
		apiKey:  apiKey,
		model:   model,
		client:  defaultHTTPClient(),
		baseURL: "https://api.openai.com",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type openaiChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	ResponseFormat *openaiFormat   `json:"response_format,omitempty"`
	Temperature    float64         `json:"temperature"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiFormat struct {
	Type string `json:"type"`
}

type openaiChatResponse struct {
	Choices []openaiChoice `json:"choices"`
	Error   *openaiError   `json:"error,omitempty"`
}

type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

type openaiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// ExtractCommitments calls OpenAI's chat-completions endpoint with
// JSON-mode response formatting. The system prompt asks for a bare
// JSON array; chat-completions JSON mode requires the word "json"
// somewhere in the prompt, so we ensure that contractually here too.
func (p *OpenAIProvider) ExtractCommitments(ctx context.Context, prompt string) ([]domain.CommitmentDraft, error) {
	if p.apiKey == "" {
		return nil, errors.New("openai: api key required")
	}

	wrapped := prompt + "\n\nReturn only a JSON object with a single key \"commitments\" mapping to the array described above."
	body, err := json.Marshal(openaiChatRequest{
		Model:          p.model,
		Temperature:    0,
		ResponseFormat: &openaiFormat{Type: "json_object"},
		Messages:       []openaiMessage{{Role: "user", Content: wrapped}},
	})
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr openaiChatResponse
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error != nil {
			return nil, fmt.Errorf("openai: %d %s: %s", resp.StatusCode, apiErr.Error.Type, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai: %d: %s", resp.StatusCode, string(raw))
	}

	var parsed openaiChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, nil
	}

	// Chat-completions JSON mode returns an object; unwrap to the
	// commitments array. Fall back to direct array parse if the
	// model ignored the wrapper instruction.
	content := parsed.Choices[0].Message.Content
	if drafts, err := parseDrafts(content); err == nil && len(drafts) > 0 {
		return drafts, nil
	}
	var wrapper struct {
		Commitments json.RawMessage `json:"commitments"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil, fmt.Errorf("openai: decode commitments: %w", err)
	}
	if len(wrapper.Commitments) == 0 {
		return nil, nil
	}
	return parseDrafts(string(wrapper.Commitments))
}
