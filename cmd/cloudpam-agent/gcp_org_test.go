package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/gcp"
	"cloudpam/internal/domain"
)

type projectCollector struct {
	project string
	fail    bool
	partial bool
	seen    *[]domain.Account
	mu      *sync.Mutex
}

func (c projectCollector) Provider() string { return "gcp" }

func (c projectCollector) Discover(_ context.Context, account domain.Account) ([]domain.DiscoveredResource, error) {
	c.mu.Lock()
	*c.seen = append(*c.seen, account)
	c.mu.Unlock()
	res := []domain.DiscoveredResource{{Provider: "gcp", ResourceType: domain.ResourceTypeVPC, ResourceID: c.project + "-net", CIDR: "10.0.0.0/16"}}
	switch {
	case c.fail:
		return nil, errors.New("compute api disabled")
	case c.partial:
		return res, errors.New("discover addresses: 403")
	}
	return res, nil
}

type orgIngestRecorder struct {
	mu   sync.Mutex
	reqs []domain.BulkIngestRequest
}

func newOrgIngestServer(t *testing.T) (*orgIngestRecorder, string) {
	t.Helper()
	rec := &orgIngestRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/discovery/ingest/org" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req domain.BulkIngestRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, req)
		rec.mu.Unlock()
		_ = json.NewEncoder(w).Encode(domain.BulkIngestResponse{AccountsProcessed: len(req.Accounts), AccountsFailed: len(req.Failures)})
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

func gcpOrgConfig() *Config {
	return &Config{
		Provider: "gcp", AccountID: 1, MaxRetries: 0, RetryBackoff: time.Millisecond,
		GCPRegions: []string{"us-central1"},
		GCPOrg:     GCPOrg{Enabled: true, Parent: "organizations/1", ExcludeProjects: []string{"sandbox"}},
	}
}

func TestRunGCPOrgSyncPushesEveryHealthyProject(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	var mu sync.Mutex
	var seen []domain.Account
	var gotParent string
	deps := gcpOrgDeps{
		listProjects: func(_ context.Context, parent string, _ *discovery.Limiter) ([]gcp.Project, error) {
			gotParent = parent
			return []gcp.Project{
				{ID: "prod", Name: "Production"},
				{ID: "sandbox", Name: "Sandbox"},
				{ID: "broken", Name: "Broken"},
				{ID: "half", Name: "Half"},
				{ID: "noauth", Name: "No Auth"},
				{ID: "dev"},
			}, nil
		},
		collectorFor: func(_ context.Context, project string, _ *discovery.Limiter) (discovery.Collector, error) {
			if project == "noauth" {
				return nil, errors.New("impersonate: 403")
			}
			return projectCollector{project: project, fail: project == "broken", partial: project == "half", seen: &seen, mu: &mu}, nil
		},
	}
	jobID := uuid.New()

	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, &jobID, covQuietLogger(), deps)

	if gotParent != "organizations/1" {
		t.Fatalf("parent = %q", gotParent)
	}
	if len(rec.reqs) != 1 {
		t.Fatalf("org pushes = %d, want 1", len(rec.reqs))
	}
	req := rec.reqs[0]
	if req.SyncJobID == nil || *req.SyncJobID != jobID {
		t.Fatalf("sync_job_id = %v, want %s", req.SyncJobID, jobID)
	}
	var ids []string
	for _, a := range req.Accounts {
		ids = append(ids, a.ExternalID)
		if a.Provider != "gcp" || a.AWSAccountID != "" || !reflect.DeepEqual(a.Regions, []string{"us-central1"}) {
			t.Fatalf("entry = %+v", a)
		}
	}
	if !reflect.DeepEqual(ids, []string{"prod", "dev"}) {
		t.Fatalf("pushed projects = %v, want [prod dev] (sandbox excluded, broken/half/noauth skipped)", ids)
	}
	if req.Accounts[0].AccountName != "Production" || req.Accounts[1].AccountName != "dev" {
		t.Fatalf("names = %q, %q", req.Accounts[0].AccountName, req.Accounts[1].AccountName)
	}
	for _, a := range seen {
		if a.Provider != "gcp" || a.ExternalID == "" {
			t.Fatalf("collector saw account %+v", a)
		}
	}
	var failed []string
	for _, f := range req.Failures {
		failed = append(failed, f.ExternalID)
		if f.Provider != "gcp" || f.Error == "" {
			t.Fatalf("failure = %+v", f)
		}
	}
	if !reflect.DeepEqual(failed, []string{"broken", "half", "noauth"}) {
		t.Fatalf("failures = %v, want [broken half noauth] (sandbox is excluded, not failed)", failed)
	}
}

func TestRunGCPOrgSyncPrefersOrgRegions(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	var mu sync.Mutex
	var seen []domain.Account
	cfg := gcpOrgConfig()
	cfg.GCPOrg.Regions = []string{"europe-west1"}
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "p"}}, nil
		},
		collectorFor: func(_ context.Context, project string, _ *discovery.Limiter) (discovery.Collector, error) {
			return projectCollector{project: project, seen: &seen, mu: &mu}, nil
		},
	}
	runGCPOrgSync(context.Background(), cfg, pusher, nil, covQuietLogger(), deps)
	if len(seen) != 1 || !reflect.DeepEqual(seen[0].Regions, []string{"europe-west1"}) {
		t.Fatalf("collector regions = %+v", seen)
	}
	if len(rec.reqs) != 1 || !reflect.DeepEqual(rec.reqs[0].Accounts[0].Regions, []string{"europe-west1"}) {
		t.Fatalf("pushed regions = %+v", rec.reqs)
	}
}

func TestRunGCPOrgSyncReportsAListingFailure(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return nil, errors.New("list folders under organizations/1: 403")
		},
		collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			t.Fatal("collectorFor must not be called when listing fails")
			return nil, nil
		},
	}
	jobID := uuid.New()
	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, &jobID, covQuietLogger(), deps)
	if len(rec.reqs) != 1 {
		t.Fatalf("org pushes = %d, want 1 reporting the listing failure", len(rec.reqs))
	}
	req := rec.reqs[0]
	if len(req.Accounts) != 0 || len(req.Failures) != 1 || req.SyncJobID == nil || *req.SyncJobID != jobID {
		t.Fatalf("request = %+v, want no accounts and one failure for the job", req)
	}
	if f := req.Failures[0]; f.Provider != "gcp" || f.ExternalID != "organizations/1" || !strings.Contains(f.Error, "list folders under organizations/1: 403") {
		t.Fatalf("failure = %+v", f)
	}
}

func TestRunGCPOrgSyncPushesNothingWhenNoProjectSucceeds(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "sandbox"}}, nil
		},
		collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			t.Fatal("excluded project must not be discovered")
			return nil, nil
		},
	}
	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, nil, covQuietLogger(), deps)
	if len(rec.reqs) != 0 {
		t.Fatalf("org pushes = %d, want 0", len(rec.reqs))
	}
}

func TestRunGCPOrgSyncCompletesARequestedJobThatFindsNothing(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "sandbox"}}, nil
		},
		collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			t.Fatal("excluded project must not be discovered")
			return nil, nil
		},
	}
	jobID := uuid.New()

	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, &jobID, covQuietLogger(), deps)

	if len(rec.reqs) != 1 {
		t.Fatalf("org pushes = %d, want 1", len(rec.reqs))
	}
	req := rec.reqs[0]
	if req.SyncJobID == nil || *req.SyncJobID != jobID || len(req.Accounts) != 0 || len(req.Failures) != 0 {
		t.Fatalf("push = %+v, want an empty report naming job %s", req, jobID)
	}
}

func TestDefaultGCPOrgDepsDirectModeUsesADCCollector(t *testing.T) {
	deps := defaultGCPOrgDeps(gcpOrgConfig())
	c, err := deps.collectorFor(context.Background(), "p", nil)
	if err != nil {
		t.Fatalf("collectorFor() error = %v", err)
	}
	if _, ok := c.(*gcp.Collector); !ok || c.Provider() != "gcp" {
		t.Fatalf("collector = %T", c)
	}
}

type stalledCollector struct{}

func (stalledCollector) Provider() string { return "gcp" }

func (stalledCollector) Discover(ctx context.Context, _ domain.Account) ([]domain.DiscoveredResource, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunGCPOrgSyncSkipsAProjectThatOutlivesItsDeadline(t *testing.T) {
	old := gcpProjectTimeout
	gcpProjectTimeout = 50 * time.Millisecond
	t.Cleanup(func() { gcpProjectTimeout = old })

	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	var mu sync.Mutex
	var seen []domain.Account
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "stuck"}, {ID: "prod"}}, nil
		},
		collectorFor: func(_ context.Context, project string, _ *discovery.Limiter) (discovery.Collector, error) {
			if project == "stuck" {
				return stalledCollector{}, nil
			}
			return projectCollector{project: project, seen: &seen, mu: &mu}, nil
		},
	}

	done := make(chan struct{})
	go func() {
		runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, nil, covQuietLogger(), deps)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runGCPOrgSync did not return; the stalled project has no deadline")
	}
	if len(rec.reqs) != 1 || len(rec.reqs[0].Accounts) != 1 || rec.reqs[0].Accounts[0].ExternalID != "prod" {
		t.Fatalf("pushes = %+v, want only prod", rec.reqs)
	}
	if f := rec.reqs[0].Failures; len(f) != 1 || f[0].ExternalID != "stuck" || !strings.Contains(f[0].Error, "deadline") {
		t.Fatalf("failures = %+v, want stuck reported as timed out", f)
	}
}

func TestRunGCPOrgSyncDoesNotReportSuccessWhenTheServerAcceptsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accounts_processed":0,"errors":["skipped account with empty aws_account_id"]}`))
	}))
	t.Cleanup(srv.Close)
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	pusher := NewPusher(srv.URL, "cpk", uuid.New(), 5*time.Second, logger)
	var mu sync.Mutex
	var seen []domain.Account
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "prod"}}, nil
		},
		collectorFor: func(_ context.Context, project string, _ *discovery.Limiter) (discovery.Collector, error) {
			return projectCollector{project: project, seen: &seen, mu: &mu}, nil
		},
	}

	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, nil, logger, deps)

	if !strings.Contains(logs.String(), "push org resources failed") || !strings.Contains(logs.String(), "0.25.0") {
		t.Fatalf("logs = %s, want the rejected push logged with the upgrade hint", logs.String())
	}
	if strings.Contains(logs.String(), "org sync completed") {
		t.Fatalf("logs = %s, must not report a completed sync", logs.String())
	}
}

func rejectedTokenError() error {
	re := &oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}, Body: []byte("secret-ish body"), ErrorCode: "invalid_grant", ErrorDescription: "secret-ish description"}
	return &url.Error{Op: "Get", URL: "https://example.googleapis.com", Err: re}
}

type tokenFailingCollector struct{}

func (tokenFailingCollector) Provider() string { return "gcp" }

func (tokenFailingCollector) Discover(context.Context, domain.Account) ([]domain.DiscoveredResource, error) {
	return nil, fmt.Errorf("list networks: %w", rejectedTokenError())
}

func TestRunGCPOrgSyncKeepsTokenEndpointBodiesOutOfFailures(t *testing.T) {
	projects := func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
		return []gcp.Project{{ID: "prod"}}, nil
	}
	cases := []struct {
		name string
		deps gcpOrgDeps
		want string
	}{
		{"listing", gcpOrgDeps{listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return nil, rejectedTokenError()
		}}, "list projects: token: invalid_grant (http 400)"},
		{"credentials", gcpOrgDeps{listProjects: projects, collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			return nil, rejectedTokenError()
		}}, "credentials: token: invalid_grant (http 400)"},
		{"discovery", gcpOrgDeps{listProjects: projects, collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			return tokenFailingCollector{}, nil
		}}, "token: invalid_grant (http 400)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, url := newOrgIngestServer(t)
			var logs strings.Builder
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, logger)

			runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, nil, logger, tc.deps)

			if len(rec.reqs) != 1 || len(rec.reqs[0].Failures) != 1 {
				t.Fatalf("pushes = %+v, want one report with one failure", rec.reqs)
			}
			if got := rec.reqs[0].Failures[0].Error; got != tc.want {
				t.Fatalf("failure error = %q, want %q", got, tc.want)
			}
			if strings.Contains(logs.String(), "secret-ish") {
				t.Fatalf("logs = %s, must not carry the token endpoint's body", logs.String())
			}
			if !strings.Contains(logs.String(), `level=WARN msg="org sync finished with failures"`) || strings.Contains(logs.String(), "org sync completed") {
				t.Fatalf("logs = %s, want a warning that the pass finished with failures", logs.String())
			}
		})
	}
}
