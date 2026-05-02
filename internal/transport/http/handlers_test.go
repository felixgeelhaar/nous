package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/validation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// seededServer returns a test server with one commitment, one
// decision, and one intervention preloaded so the listing handlers
// have rows to return.
func seededServer(t *testing.T) (*Server, domain.Commitment, domain.Intervention, domain.Decision) {
	t.Helper()
	srv := newTestServer(t)
	ctx := context.Background()

	now := time.Now().UTC()
	c, err := domain.NewCommitment("alice", "ship report", nil, 0.9, now)
	require.NoError(t, err)
	require.NoError(t, srv.commitments.Save(ctx, c))

	d, err := domain.NewDecision("commitment.extract", "saved 1 draft", 1.0, now)
	require.NoError(t, err)
	require.NoError(t, srv.decisions.Save(ctx, d))

	iv, err := domain.NewIntervention(domain.InterventionNudge, "nudge alice", &c.ID, nil, nil, now)
	require.NoError(t, err)
	require.NoError(t, srv.intervs.Save(ctx, iv))

	return srv, c, iv, d
}

func TestHTTP_ListCommitments(t *testing.T) {
	t.Parallel()
	srv, _, _, _ := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/commitments?owner_id=alice")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out []commitmentJSON
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.GreaterOrEqual(t, len(out), 1)
}

func TestHTTP_GetCommitment(t *testing.T) {
	t.Parallel()
	srv, c, _, _ := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/commitments/" + c.ID.String())
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out commitmentJSON
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.Equal(t, c.ID.String(), out.ID)
}

func TestHTTP_GetCommitment_BadID(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/commitments/not-a-uuid")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestHTTP_GetCommitment_NotFound(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/commitments/" + uuid.New().String())
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestHTTP_Evaluate(t *testing.T) {
	t.Parallel()
	srv, _, _, _ := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Post(ts.URL+"/v1/evaluate", "application/json", nil)
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out evaluateResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.GreaterOrEqual(t, out.Evaluated, 0)
}

func TestHTTP_ListDecisions(t *testing.T) {
	t.Parallel()
	srv, _, _, _ := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/decisions?subject=commitment.extract&limit=10")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out []decisionJSON
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.GreaterOrEqual(t, len(out), 1)
}

func TestHTTP_ListDecisions_RequiresSubject(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/decisions")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestHTTP_GetDecision(t *testing.T) {
	t.Parallel()
	srv, _, _, d := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/decisions/" + d.ID.String())
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out decisionJSON
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.Equal(t, d.ID.String(), out.ID)
}

func TestHTTP_GetDecision_BadID(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/decisions/not-a-uuid")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestHTTP_GetDecision_NotFound(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/decisions/" + uuid.New().String())
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestHTTP_ListInterventions(t *testing.T) {
	t.Parallel()
	srv, c, _, _ := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/interventions?commitment_id=" + c.ID.String())
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out []interventionJSON
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.GreaterOrEqual(t, len(out), 1)
}

func TestHTTP_ListInterventions_BadID(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/interventions?commitment_id=not-a-uuid")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestHTTP_ResolveIntervention(t *testing.T) {
	t.Parallel()
	srv, _, iv, _ := seededServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(resolveRequest{Status: string(domain.InterventionAccepted)})
	res, err := http.Post(ts.URL+"/v1/interventions/"+iv.ID.String()+"/resolve",
		"application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var out interventionJSON
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	require.Equal(t, string(domain.InterventionAccepted), out.Status)
}

func TestHTTP_ResolveIntervention_NotFound(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(resolveRequest{Status: "accepted"})
	res, err := http.Post(ts.URL+"/v1/interventions/"+uuid.New().String()+"/resolve",
		"application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestHTTP_Health(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/health")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	body, _ := io.ReadAll(res.Body)
	require.Contains(t, string(body), "healthy")
}

func TestHTTP_Health_WithFailingChecker(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	srv.healthChecker = func(_ context.Context) error { return io.EOF }
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/v1/health")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
}

func TestHTTP_Metrics(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/metrics")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)
}

func TestHTTP_ExtractWithValidator(t *testing.T) {
	t.Parallel()
	v, err := validation.NewValidator([]validation.Rule{
		{Name: "max_text", Expr: "text_len <= 50"},
	})
	require.NoError(t, err)

	srv := newTestServer(t).WithValidator(v)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	tooLong := make([]byte, 200)
	for i := range tooLong {
		tooLong[i] = 'x'
	}
	body, _ := json.Marshal(extractRequest{OwnerID: "u1", Text: string(tooLong)})
	res, err := http.Post(ts.URL+"/v1/extract", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestHTTP_ExtractRejectsMissingFields(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(extractRequest{OwnerID: "u1"})
	res, err := http.Post(ts.URL+"/v1/extract", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestHTTP_CombinedAuthMiddleware_StaticTokenAccepted(t *testing.T) {
	t.Parallel()
	called := false
	h := CombinedAuthMiddleware("secret", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/commitments", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.True(t, called)
}

func TestHTTP_CombinedAuthMiddleware_RejectsBoth(t *testing.T) {
	t.Parallel()
	h := CombinedAuthMiddleware("secret", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler called")
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/commitments", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHTTP_CombinedAuthMiddleware_DisabledWhenBothEmpty(t *testing.T) {
	t.Parallel()
	called := false
	h := CombinedAuthMiddleware("", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/commitments", nil))
	require.True(t, called)
}
