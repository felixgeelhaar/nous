// Package e2e_test provides end-to-end tests over the HTTP API.
package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
	httpserver "github.com/felixgeelhaar/nous/internal/transport/http"
	"github.com/stretchr/testify/require"
)

func setupHTTPServer(t *testing.T) (*httptest.Server, *memory.Conn) {
	t.Helper()
	store := memory.New()
	riskEngine := risk.New(risk.DefaultConfig())
	interventionEngine := intervention.New(intervention.DefaultConfig())

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
		Risk:          riskEngine,
		Intervention:  interventionEngine,
	})
	require.NoError(t, err)

	srv := httpserver.NewServer(extractor, eval, store.Commitments, store.Decisions, store.Interventions, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })
	return ts, store
}

func TestHTTP_GetCommitment(t *testing.T) {
	_, _ = setupHTTPServer(t)

	// TODO: implement once HTTP server is fully wired
	t.Skip("HTTP get commitment test not yet implemented")
}

func TestHTTP_Extract(t *testing.T) {
	srv, _ := setupHTTPServer(t)

	body := `{"owner_id":"test-user","text":"I will follow up with the client tomorrow"}`
	resp, err := http.Post(srv.URL+"/v1/extract", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}
