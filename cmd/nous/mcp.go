package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	mcp "github.com/felixgeelhaar/mcp-go"
)

// mcpServerVersion is what nous reports to MCP clients. Bumped
// alongside the binary's release tag.
const mcpServerVersion = "0.3.0"

// runMCP starts the Nous MCP stdio server.
//
// The MCP surface proxies through Nous's HTTP API — every tool call
// is one HTTP round-trip to a running `nous` process. Same pipeline
// as the HTTP and gRPC surfaces; the wire-shape is the only thing
// that changes. Configure via `NOUS_HTTP_URL` (default
// `http://localhost:8080`) and `NOUS_AUTH_TOKEN` if the upstream
// requires bearer auth.
func runMCP() error {
	base := envOr("NOUS_HTTP_URL", "http://localhost:8080")
	token := os.Getenv("NOUS_AUTH_TOKEN")
	hc := &http.Client{Timeout: 30 * time.Second}

	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "nous",
		Version: mcpServerVersion,
		Capabilities: mcp.Capabilities{
			Tools: true,
		},
	},
		mcp.WithTitle("Nous MCP Server"),
		mcp.WithDescription("Track AI commitments, score risk, and surface interventions before deadlines slip. Backed by a running nous HTTP server."),
		mcp.WithWebsiteURL("https://github.com/felixgeelhaar/nous"),
		mcp.WithInstructions("Use nous_extract to extract a commitment from prose. Use nous_evaluate to advance the risk engine one tick. Use nous_list_interventions / nous_list_decisions to read the audit chain. All four tools talk to a running nous server at NOUS_HTTP_URL."),
	)

	srv.Tool("nous_extract").
		Description("Extract a commitment from prose and run the standard extractor pipeline. Returns the saved commitment IDs and the decision_id of the extraction record.").
		OutputSchema(mcpExtractOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, in mcpExtractInput) (mcpExtractOutput, error) {
			body, _ := json.Marshal(map[string]any{
				"owner_id":    in.OwnerID,
				"text":        in.Text,
				"hints":       in.Hints,
				"source_refs": in.SourceRefs,
			})
			var out mcpExtractOutput
			if err := mcpHTTP(ctx, hc, http.MethodPost, base+"/v1/extract", token, bytes.NewReader(body), &out); err != nil {
				return mcpExtractOutput{}, err
			}
			return out, nil
		})

	srv.Tool("nous_evaluate").
		Description("Advance the risk engine one tick. Scores every active commitment against current signals and emits interventions when risk crosses the configured thresholds.").
		OutputSchema(mcpEvaluateOutput{}).
		Handler(func(ctx context.Context, _ struct{}) (mcpEvaluateOutput, error) {
			var out mcpEvaluateOutput
			if err := mcpHTTP(ctx, hc, http.MethodPost, base+"/v1/evaluate", token, nil, &out); err != nil {
				return mcpEvaluateOutput{}, err
			}
			return out, nil
		})

	srv.Tool("nous_list_interventions").
		Description("List interventions emitted by the risk engine. Optional status filter (open / acknowledged / resolved) and a result limit.").
		OutputSchema(mcpListInterventionsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, in mcpListInterventionsInput) (mcpListInterventionsOutput, error) {
			q := url.Values{}
			if in.Status != "" {
				q.Set("status", in.Status)
			}
			if in.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", in.Limit))
			}
			u := base + "/v1/interventions"
			if len(q) > 0 {
				u += "?" + q.Encode()
			}
			var raw []map[string]any
			if err := mcpHTTP(ctx, hc, http.MethodGet, u, token, nil, &raw); err != nil {
				return mcpListInterventionsOutput{}, err
			}
			return mcpListInterventionsOutput{Interventions: raw, Count: len(raw)}, nil
		})

	srv.Tool("nous_list_decisions").
		Description("List recorded decisions from the replayable decision log. Filter by subject (commitment_id or 'commitment.evaluate') or by decision_id directly.").
		OutputSchema(mcpListDecisionsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, in mcpListDecisionsInput) (mcpListDecisionsOutput, error) {
			if in.ID != "" {
				var raw map[string]any
				if err := mcpHTTP(ctx, hc, http.MethodGet, base+"/v1/decisions/"+url.PathEscape(in.ID), token, nil, &raw); err != nil {
					return mcpListDecisionsOutput{}, err
				}
				return mcpListDecisionsOutput{Decisions: []map[string]any{raw}, Count: 1}, nil
			}
			q := url.Values{}
			if in.Subject != "" {
				q.Set("subject", in.Subject)
			}
			if in.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", in.Limit))
			}
			u := base + "/v1/decisions"
			if len(q) > 0 {
				u += "?" + q.Encode()
			}
			var raw []map[string]any
			if err := mcpHTTP(ctx, hc, http.MethodGet, u, token, nil, &raw); err != nil {
				return mcpListDecisionsOutput{}, err
			}
			return mcpListDecisionsOutput{Decisions: raw, Count: len(raw)}, nil
		})

	return mcp.ServeStdio(context.Background(), srv)
}

func mcpHTTP(ctx context.Context, hc *http.Client, method, u, token string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("nous mcp: http %s %s: %w", method, u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("nous mcp: %s %s -> %d: %s", method, u, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("nous mcp: decode %s response: %w", u, err)
	}
	return nil
}

// --- MCP I/O types ---

type mcpExtractInput struct {
	OwnerID    string             `json:"owner_id" jsonschema:"required,description=Caller / owner identity for rate limiting and per-owner audit"`
	Text       string             `json:"text" jsonschema:"required,description=Prose containing one or more commitments to extract"`
	Hints      []string           `json:"hints,omitempty" jsonschema:"description=Optional extractor hints (e.g. 'meeting-notes')"`
	SourceRefs []mcpExtractSource `json:"source_refs,omitempty" jsonschema:"description=Origin references stored on the commitment for evidence"`
}

type mcpExtractSource struct {
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
}

type mcpExtractOutput struct {
	Considered int      `json:"considered"`
	SavedIDs   []string `json:"saved_ids"`
	Dropped    int      `json:"dropped"`
	DecisionID string   `json:"decision_id,omitempty"`
}

type mcpEvaluateOutput struct {
	Evaluated       int      `json:"evaluated"`
	Updated         int      `json:"updated"`
	InterventionIDs []string `json:"intervention_ids,omitempty"`
	DecisionIDs     []string `json:"decision_ids,omitempty"`
}

type mcpListInterventionsInput struct {
	Status string `json:"status,omitempty" jsonschema:"description=Optional status filter (open / acknowledged / resolved)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Result cap (default unlimited; server may enforce its own ceiling)"`
}

type mcpListInterventionsOutput struct {
	Interventions []map[string]any `json:"interventions"`
	Count         int              `json:"count"`
}

type mcpListDecisionsInput struct {
	Subject string `json:"subject,omitempty" jsonschema:"description=Optional subject filter (e.g. commitment.evaluate or a specific commitment_id)"`
	ID      string `json:"id,omitempty" jsonschema:"description=Fetch a single decision by ID; takes precedence over subject + limit"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=Result cap"`
}

type mcpListDecisionsOutput struct {
	Decisions []map[string]any `json:"decisions"`
	Count     int              `json:"count"`
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
