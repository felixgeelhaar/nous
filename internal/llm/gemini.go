package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// GeminiProvider implements Provider using Google's Generative
// Language API (Gemini).
type GeminiProvider struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
}

// GeminiOption tunes a provider after construction.
type GeminiOption func(*GeminiProvider)

// WithGeminiHTTPClient overrides the default HTTP client.
func WithGeminiHTTPClient(c *http.Client) GeminiOption {
	return func(p *GeminiProvider) { p.client = c }
}

// WithGeminiBaseURL points the provider at a non-default endpoint.
func WithGeminiBaseURL(u string) GeminiOption {
	return func(p *GeminiProvider) { p.baseURL = u }
}

// NewGeminiProvider creates a new Gemini LLM provider.
func NewGeminiProvider(apiKey, model string, opts ...GeminiOption) *GeminiProvider {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	p := &GeminiProvider{
		apiKey:  apiKey,
		model:   model,
		client:  defaultHTTPClient(),
		baseURL: "https://generativelanguage.googleapis.com",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type geminiRequest struct {
	Contents         []geminiContent          `json:"contents"`
	GenerationConfig *geminiGenerationConfig  `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature      float64 `json:"temperature"`
	ResponseMIMEType string  `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ExtractCommitments calls Gemini and parses the JSON reply.
func (p *GeminiProvider) ExtractCommitments(ctx context.Context, prompt string) ([]domain.CommitmentDraft, error) {
	if p.apiKey == "" {
		return nil, errors.New("gemini: api key required")
	}

	body, err := json.Marshal(geminiRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: prompt}},
		}},
		GenerationConfig: &geminiGenerationConfig{
			Temperature:      0,
			ResponseMIMEType: "application/json",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		p.baseURL, url.PathEscape(p.model), url.QueryEscape(p.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr geminiResponse
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error != nil {
			return nil, fmt.Errorf("gemini: %d %s: %s", resp.StatusCode, apiErr.Error.Status, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("gemini: %d: %s", resp.StatusCode, string(raw))
	}

	var parsed geminiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("gemini: decode response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, nil
	}
	var text string
	for _, part := range parsed.Candidates[0].Content.Parts {
		text += part.Text
	}
	return parseDrafts(text)
}
