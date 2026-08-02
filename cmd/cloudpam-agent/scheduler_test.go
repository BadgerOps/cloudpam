package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// covStubCollector is a hermetic stand-in for the AWS collector: it records the
// account it was asked to discover and returns canned resources.
type covStubCollector struct {
	mu        sync.Mutex
	calls     int
	accounts  []domain.Account
	resources []domain.DiscoveredResource
	err       error
	notify    chan struct{}
}

func (c *covStubCollector) Provider() string { return "aws" }

func (c *covStubCollector) Discover(_ context.Context, account domain.Account) ([]domain.DiscoveredResource, error) {
	c.mu.Lock()
	c.calls++
	c.accounts = append(c.accounts, account)
	notify := c.notify
	c.mu.Unlock()
	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return c.resources, nil
}

func (c *covStubCollector) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *covStubCollector) lastAccount() domain.Account {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.accounts) == 0 {
		return domain.Account{}
	}
	return c.accounts[len(c.accounts)-1]
}

// covIngestServer collects ingest and heartbeat requests from the scheduler.
type covIngestServer struct {
	mu         sync.Mutex
	ingests    []domain.IngestRequest
	heartbeats []domain.AgentHeartbeatRequest

	ingested   chan struct{}
	beat       chan struct{}
	beatSyncID *uuid.UUID
}

func (s *covIngestServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/discovery/ingest":
			var req domain.IngestRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.mu.Lock()
			s.ingests = append(s.ingests, req)
			s.mu.Unlock()
			select {
			case s.ingested <- struct{}{}:
			default:
			}
			_ = json.NewEncoder(w).Encode(domain.IngestResponse{ResourcesFound: len(req.Resources)})
		case "/api/v1/discovery/agents/heartbeat":
			var req domain.AgentHeartbeatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.mu.Lock()
			s.heartbeats = append(s.heartbeats, req)
			syncID := s.beatSyncID
			s.mu.Unlock()
			select {
			case s.beat <- struct{}{}:
			default:
			}
			_ = json.NewEncoder(w).Encode(domain.AgentHeartbeatResponse{Status: "ok", SyncJobID: syncID, AccountID: req.AccountID})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (s *covIngestServer) ingestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ingests)
}

func (s *covIngestServer) heartbeatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.heartbeats)
}

func (s *covIngestServer) allIngests() []domain.IngestRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.IngestRequest(nil), s.ingests...)
}

func (s *covIngestServer) allHeartbeats() []domain.AgentHeartbeatRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.AgentHeartbeatRequest(nil), s.heartbeats...)
}

func covNewIngestServer(t *testing.T) (*covIngestServer, string) {
	t.Helper()
	rec := &covIngestServer{
		ingested: make(chan struct{}, 16),
		beat:     make(chan struct{}, 16),
	}
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

func covWaitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestCovRunSyncDiscoversAndPushes(t *testing.T) {
	rec, url := covNewIngestServer(t)
	collector := &covStubCollector{resources: []domain.DiscoveredResource{
		{ID: uuid.New(), Provider: "aws", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-a", CIDR: "10.0.0.0/16"},
	}}
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	syncJobID := uuid.New()

	cfg := &Config{AccountID: 55, AWSRegions: []string{"us-east-1", "us-west-2"}, MaxRetries: 0, RetryBackoff: time.Millisecond}
	runSync(context.Background(), cfg, collector, pusher, &syncJobID, covQuietLogger())

	if collector.callCount() != 1 {
		t.Fatalf("collector calls = %d, want 1", collector.callCount())
	}
	acct := collector.lastAccount()
	if acct.ID != 55 {
		t.Errorf("discovered account ID = %d, want 55", acct.ID)
	}
	if acct.Provider != "aws" {
		t.Errorf("discovered account Provider = %q, want aws", acct.Provider)
	}
	if len(acct.Regions) != 2 || acct.Regions[0] != "us-east-1" {
		t.Errorf("discovered account Regions = %v", acct.Regions)
	}

	ingests := rec.allIngests()
	if len(ingests) != 1 {
		t.Fatalf("ingest requests = %d, want 1", len(ingests))
	}
	if ingests[0].AccountID != 55 {
		t.Errorf("ingest account_id = %d, want 55", ingests[0].AccountID)
	}
	if ingests[0].SyncJobID == nil || *ingests[0].SyncJobID != syncJobID {
		t.Errorf("ingest sync_job_id = %v, want %s", ingests[0].SyncJobID, syncJobID)
	}
	if len(ingests[0].Resources) != 1 || ingests[0].Resources[0].ResourceID != "vpc-a" {
		t.Errorf("ingest resources = %+v", ingests[0].Resources)
	}
}

func TestCovRunSyncSkipsPushWhenDiscoveryFails(t *testing.T) {
	rec, url := covNewIngestServer(t)
	collector := &covStubCollector{err: errors.New("ec2:DescribeVpcs denied")}
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())

	cfg := &Config{AccountID: 7, MaxRetries: 0, RetryBackoff: time.Millisecond}
	runSync(context.Background(), cfg, collector, pusher, nil, covQuietLogger())

	if collector.callCount() != 1 {
		t.Fatalf("collector calls = %d, want 1", collector.callCount())
	}
	if rec.ingestCount() != 0 {
		t.Fatalf("ingest requests = %d, want 0 when discovery fails", rec.ingestCount())
	}
}

func TestCovRunSyncToleratesPushFailure(t *testing.T) {
	var calls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad", "detail": "nope"})
	}))
	defer srv.Close()

	collector := &covStubCollector{resources: []domain.DiscoveredResource{{ID: uuid.New(), ResourceID: "vpc-a"}}}
	pusher := NewPusher(srv.URL, "cpk", uuid.New(), 5*time.Second, covQuietLogger())

	cfg := &Config{AccountID: 3, MaxRetries: 2, RetryBackoff: time.Millisecond}
	runSync(context.Background(), cfg, collector, pusher, nil, covQuietLogger())

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("push attempts = %d, want 1 (a 4xx must not be retried)", calls)
	}
}

func TestCovRunSchedulerRunsInitialSyncAndHeartbeatThenStopsOnCancel(t *testing.T) {
	rec, url := covNewIngestServer(t)
	collector := &covStubCollector{resources: []domain.DiscoveredResource{{ID: uuid.New(), ResourceID: "vpc-init"}}}
	agentID := uuid.New()
	pusher := NewPusher(url, "cpk", agentID, 5*time.Second, covQuietLogger())

	cfg := &Config{
		AgentName:         "sched-agent",
		AccountID:         12,
		SyncInterval:      time.Hour,
		HeartbeatInterval: time.Hour,
		MaxRetries:        0,
		RetryBackoff:      time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runScheduler(ctx, cfg, collector, pusher, agentID, "host-sched", covQuietLogger())
	}()

	covWaitFor(t, rec.ingested, "initial sync ingest")
	covWaitFor(t, rec.beat, "initial heartbeat")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runScheduler() error = %v, want nil on cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runScheduler did not return after context cancellation")
	}

	beats := rec.allHeartbeats()
	if len(beats) == 0 {
		t.Fatal("no heartbeats recorded")
	}
	if beats[0].AgentID != agentID || beats[0].Name != "sched-agent" || beats[0].AccountID != 12 || beats[0].Hostname != "host-sched" {
		t.Fatalf("heartbeat payload = %+v", beats[0])
	}
	if beats[0].Version != version {
		t.Errorf("heartbeat version = %q, want %q", beats[0].Version, version)
	}
}

func TestCovRunSchedulerSkipsHeartbeatWhenAccountIDUnset(t *testing.T) {
	rec, url := covNewIngestServer(t)
	collector := &covStubCollector{notify: make(chan struct{}, 4)}
	agentID := uuid.New()
	pusher := NewPusher(url, "cpk", agentID, 5*time.Second, covQuietLogger())

	cfg := &Config{
		AgentName:         "no-account",
		AccountID:         0,
		SyncInterval:      time.Hour,
		HeartbeatInterval: 5 * time.Millisecond,
		MaxRetries:        0,
		RetryBackoff:      time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runScheduler(ctx, cfg, collector, pusher, agentID, "host", covQuietLogger())
	}()

	covWaitFor(t, collector.notify, "initial sync")
	// Give the heartbeat ticker several chances to fire before cancelling.
	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runScheduler() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runScheduler did not return after context cancellation")
	}

	if got := rec.heartbeatCount(); got != 0 {
		t.Fatalf("heartbeats = %d, want 0 when account_id is unset", got)
	}
}

func TestCovRunSchedulerRunsServerRequestedSync(t *testing.T) {
	rec, url := covNewIngestServer(t)
	requested := uuid.New()
	rec.beatSyncID = &requested

	collector := &covStubCollector{resources: []domain.DiscoveredResource{{ID: uuid.New(), ResourceID: "vpc-req"}}}
	agentID := uuid.New()
	pusher := NewPusher(url, "cpk", agentID, 5*time.Second, covQuietLogger())

	cfg := &Config{
		AgentName:         "req-agent",
		AccountID:         31,
		SyncInterval:      time.Hour,
		HeartbeatInterval: 5 * time.Millisecond,
		MaxRetries:        0,
		RetryBackoff:      time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runScheduler(ctx, cfg, collector, pusher, agentID, "host", covQuietLogger())
	}()

	// Wait until an ingest carries the server-requested sync job id.
	deadline := time.After(5 * time.Second)
	found := false
	for !found {
		select {
		case <-rec.ingested:
			for _, ing := range rec.allIngests() {
				if ing.SyncJobID != nil && *ing.SyncJobID == requested {
					found = true
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the server-requested sync")
		}
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runScheduler() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runScheduler did not return after context cancellation")
	}
}

func TestCovRunSchedulerContinuesAfterHeartbeatFailure(t *testing.T) {
	var mu sync.Mutex
	var heartbeats int
	ingested := make(chan struct{}, 8)
	beat := make(chan struct{}, 8)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/discovery/agents/heartbeat":
			mu.Lock()
			heartbeats++
			mu.Unlock()
			select {
			case beat <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusInternalServerError)
		default:
			select {
			case ingested <- struct{}{}:
			default:
			}
			_ = json.NewEncoder(w).Encode(domain.IngestResponse{})
		}
	}))
	defer srv.Close()

	collector := &covStubCollector{}
	agentID := uuid.New()
	pusher := NewPusher(srv.URL, "cpk", agentID, 5*time.Second, covQuietLogger())

	cfg := &Config{
		AgentName:         "flaky",
		AccountID:         4,
		SyncInterval:      time.Hour,
		HeartbeatInterval: 5 * time.Millisecond,
		MaxRetries:        0,
		RetryBackoff:      time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runScheduler(ctx, cfg, collector, pusher, agentID, "host", covQuietLogger())
	}()

	// Two heartbeats prove the scheduler survived the first failure.
	covWaitFor(t, beat, "first heartbeat")
	covWaitFor(t, beat, "second heartbeat")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runScheduler() error = %v, want nil despite heartbeat failures", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runScheduler did not return after context cancellation")
	}

	mu.Lock()
	defer mu.Unlock()
	if heartbeats < 2 {
		t.Fatalf("heartbeats = %d, want at least 2", heartbeats)
	}
}
