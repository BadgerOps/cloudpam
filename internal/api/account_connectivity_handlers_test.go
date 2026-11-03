package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

type probingCollector struct{ err error }

func (probingCollector) Provider() string { return "aws" }

func (probingCollector) Discover(context.Context, domain.Account) ([]domain.DiscoveredResource, error) {
	return nil, nil
}

func (c probingCollector) Identity(context.Context, domain.Account) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return "arn:aws:iam::123456789012:role/host", nil
}

type connectivityFixture struct {
	mux   *http.ServeMux
	store *storage.MemoryStore
	audit *audit.MemoryAuditLogger
}

func newConnectivityFixture(t *testing.T, collector discovery.Collector) connectivityFixture {
	t.Helper()
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	auditLogger := audit.NewMemoryAuditLogger()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, auditLogger)
	ds := storage.NewMemoryDiscoveryStore(st)
	syncSvc := discovery.NewSyncService(ds)
	syncSvc.RegisterCollector(collector)
	NewDiscoveryServer(srv, ds, syncSvc, nil).RegisterDiscoveryRoutes()
	return connectivityFixture{mux: mux, store: st, audit: auditLogger}
}

func (f connectivityFixture) account(t *testing.T, provider string) domain.Account {
	t.Helper()
	a, err := f.store.CreateAccount(context.Background(), domain.CreateAccount{Key: provider + ":acct", Name: "acct", Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f connectivityFixture) test(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+id+"/test", nil))
	return rr
}

func TestAccountConnectivityTestReportsStatusLatencyAndIdentity(t *testing.T) {
	f := newConnectivityFixture(t, probingCollector{})
	a := f.account(t, "aws")

	rr := f.test(t, fmt.Sprint(a.ID))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["source"] != "host" || body["identity"] != "arn:aws:iam::123456789012:role/host" {
		t.Fatalf("body = %v", body)
	}
	if _, ok := body["latency_ms"].(float64); !ok {
		t.Fatalf("latency_ms missing: %v", body)
	}
	if _, ok := body["tested_at"].(string); !ok {
		t.Fatalf("tested_at missing: %v", body)
	}

	events, _, err := f.audit.List(context.Background(), audit.ListOptions{Action: audit.ActionTest})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ResourceType != audit.ResourceAccount || events[0].ResourceID != fmt.Sprint(a.ID) {
		t.Fatalf("audit events = %+v", events)
	}
	if after := events[0].Changes.After; after["status"] != domain.CredentialTestOK || after["source"] != domain.ConnectivitySourceHost {
		t.Fatalf("audit changes = %+v", events[0].Changes)
	}
}

func TestAccountConnectivityTestReturnsAFailedProbeAs200(t *testing.T) {
	f := newConnectivityFixture(t, probingCollector{err: &discovery.ProbeError{Step: discovery.ProbeStepGetCallerIdentity, Status: 403, Code: "InvalidClientTokenId"}})
	a := f.account(t, "aws")

	rr := f.test(t, fmt.Sprint(a.ID))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	var res domain.ConnectivityTestResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.CredentialTestFailed || res.Identity != "" || res.Error != "get_caller_identity: InvalidClientTokenId (http 403)" {
		t.Fatalf("result = %+v", res)
	}
	events, _, _ := f.audit.List(context.Background(), audit.ListOptions{Action: audit.ActionTest})
	if len(events) != 1 || events[0].Changes.After["status"] != domain.CredentialTestFailed {
		t.Fatalf("audit events = %+v", events)
	}
}

func TestAccountConnectivityTestRejects(t *testing.T) {
	f := newConnectivityFixture(t, probingCollector{})
	onPrem := f.account(t, "on-prem")
	for _, tc := range []struct {
		name string
		id   string
		want int
	}{
		{"a non-numeric id", "abc", http.StatusBadRequest},
		{"an unknown account", "999", http.StatusNotFound},
		{"a provider without a collector", fmt.Sprint(onPrem.ID), http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rr := f.test(t, tc.id); rr.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", rr.Code, tc.want, rr.Body)
			}
		})
	}
	if events, _, _ := f.audit.List(context.Background(), audit.ListOptions{Action: audit.ActionTest}); len(events) != 0 {
		t.Fatalf("rejected tests were audited: %+v", events)
	}
}
