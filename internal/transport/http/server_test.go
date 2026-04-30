package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) *Server {
	ctx := t.Context()
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

	now := time.Now().UTC()
	due := now.Add(-time.Hour) // overdue so risk is high
	c, err := domain.NewCommitment("test-owner", "finish the report", &due, 0.9, now)
	require.NoError(t, err)
	require.NoError(t, store.Commitments.Save(ctx, c))

	return NewServer(extractor, eval, store.Commitments, store.Decisions, store.Interventions, nil, nil)
}

func TestHTTP_Extract(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(func() { srv.Close() })

	body, _ := json.Marshal(extractRequest{
		OwnerID: "alice",
		Text:    "I will call the client tomorrow.",
	})
	res, err := http.Post(srv.URL+"/v1/extract", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })

	require.Equal(t, http.StatusOK, res.StatusCode)

	var out extractResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.GreaterOrEqual(t, out.Considered, 0)
	require.GreaterOrEqual(t, len(out.SavedIDs)+out.Dropped, 0)
}
