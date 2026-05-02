package praxis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/google/uuid"
)

// fakePraxis stands in for a deployed Praxis service. Each handler
// records what it received so tests can assert wire shape without
// importing praxis/internal/domain.
type fakePraxis struct {
	listResp       []capabilityWire
	executeResp    resultWire
	dryRunResp     simulationWire
	statusCode     int
	gotPath        string
	gotAuth        string
	gotIdempotency string
	gotCapability  string
}

func (f *fakePraxis) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		f.gotPath = r.URL.Path
		f.gotAuth = r.Header.Get("Authorization")
		if f.statusCode != 0 {
			w.WriteHeader(f.statusCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": f.listResp})
	})
	mux.HandleFunc("/v1/actions", func(w http.ResponseWriter, r *http.Request) {
		f.gotPath = r.URL.Path
		f.gotAuth = r.Header.Get("Authorization")
		var act actionWire
		_ = json.NewDecoder(r.Body).Decode(&act)
		f.gotIdempotency = act.IdempotencyKey
		f.gotCapability = act.Capability
		if f.statusCode != 0 {
			w.WriteHeader(f.statusCode)
			return
		}
		f.executeResp.ActionID = act.ID
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.executeResp)
	})
	mux.HandleFunc("/v1/actions/", func(w http.ResponseWriter, r *http.Request) {
		f.gotPath = r.URL.Path
		f.gotAuth = r.Header.Get("Authorization")
		if f.statusCode != 0 {
			w.WriteHeader(f.statusCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.dryRunResp)
	})
	return mux
}

func TestAdapter_ListCapabilities_Live(t *testing.T) {
	t.Parallel()
	fake := &fakePraxis{
		listResp: []capabilityWire{
			{Name: "deploy", Description: "deploy something", InputSchema: map[string]any{"type": "object"}},
			{Name: "rollback", Description: "rollback a deploy"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := NewAdapter(Config{Addr: srv.URL, BearerToken: "secret"})
	caps, err := a.ListCapabilities(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(caps) != 2 {
		t.Errorf("len = %d, want 2", len(caps))
	}
	if caps[0].Name != "deploy" {
		t.Errorf("first = %q", caps[0].Name)
	}
	if fake.gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", fake.gotAuth)
	}
	if a.AdapterStatus() != "healthy" {
		t.Errorf("status = %q", a.AdapterStatus())
	}
}

func TestAdapter_Execute_Live(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	fake := &fakePraxis{
		executeResp: resultWire{
			Status:      "succeeded",
			Output:      map[string]any{"deploy_id": "d_42"},
			StartedAt:   now,
			CompletedAt: now.Add(2 * time.Second),
			Attempts:    1,
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := NewAdapter(Config{Addr: srv.URL})
	req := domain.ActionRequest{
		ID:         uuid.New(),
		Capability: "deploy",
		Payload:    map[string]any{"env": "prod"},
	}
	res, err := a.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Success {
		t.Errorf("not success: %+v", res)
	}
	if res.Output["deploy_id"] != "d_42" {
		t.Errorf("output = %+v", res.Output)
	}
	if res.IdempotencyKey == "" {
		t.Error("idempotency key not auto-filled")
	}
	if fake.gotIdempotency == "" {
		t.Error("idempotency key not sent on wire")
	}
	if fake.gotCapability != "deploy" {
		t.Errorf("capability sent = %q", fake.gotCapability)
	}
}

func TestAdapter_DryRun_Live(t *testing.T) {
	t.Parallel()
	fake := &fakePraxis{
		dryRunResp: simulationWire{
			Validation: validationWire{Valid: false, Errors: []string{"env required"}},
			Preview:    map[string]any{"affects": "prod"},
			Reversible: false,
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := NewAdapter(Config{Addr: srv.URL})
	req := domain.ActionRequest{Capability: "deploy", Payload: map[string]any{"env": ""}}
	sim, err := a.DryRun(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sim.Blockers) != 1 || sim.Blockers[0] != "env required" {
		t.Errorf("blockers = %+v", sim.Blockers)
	}
	if !strings.Contains(fake.gotPath, "/dry-run") {
		t.Errorf("dry-run path mismatch: %q", fake.gotPath)
	}
}

func TestAdapter_Execute_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()
	fake := &fakePraxis{statusCode: http.StatusBadGateway}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := NewAdapter(Config{Addr: srv.URL})
	req := domain.ActionRequest{ID: uuid.New(), Capability: "deploy"}
	_, err := a.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("err = %v", err)
	}
}

func TestAdapter_Execute_ExecuteFailureSurfacesErrorMessage(t *testing.T) {
	t.Parallel()
	fake := &fakePraxis{
		executeResp: resultWire{
			Status: "failed",
			Error:  &errorWire{Code: "policy_denied", Message: "scope mismatch"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := NewAdapter(Config{Addr: srv.URL})
	req := domain.ActionRequest{ID: uuid.New(), Capability: "deploy"}
	res, err := a.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Success {
		t.Error("should not be success")
	}
	if res.Error != "scope mismatch" {
		t.Errorf("error = %q", res.Error)
	}
}
