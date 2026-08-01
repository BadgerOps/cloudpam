package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func covQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// covRecorder captures the requests an httptest server receives so assertions
// can run after the call under test returns.
type covRecorder struct {
	mu      sync.Mutex
	paths   []string
	methods []string
	auths   []string
	types   []string
	bodies  [][]byte
}

func (r *covRecorder) record(req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, req.URL.Path)
	r.methods = append(r.methods, req.Method)
	r.auths = append(r.auths, req.Header.Get("Authorization"))
	r.types = append(r.types, req.Header.Get("Content-Type"))
	r.bodies = append(r.bodies, body)
}

func (r *covRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *covRecorder) body(i int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[i]
}

func (r *covRecorder) snapshot() (paths, methods, auths, types []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...), append([]string(nil), r.methods...),
		append([]string(nil), r.auths...), append([]string(nil), r.types...)
}

func covNewPusherFor(t *testing.T, handler http.HandlerFunc) (*Pusher, uuid.UUID, *covRecorder) {
	t.Helper()
	rec := &covRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	agentID := uuid.New()
	return NewPusher(srv.URL, "cpk_secret", agentID, 5*time.Second, covQuietLogger()), agentID, rec
}

// covDeadServerPusher points at an address with nothing listening so the very
// first transport attempt fails.
func covDeadServerPusher(t *testing.T) *Pusher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return NewPusher(url, "cpk_secret", uuid.New(), time.Second, covQuietLogger())
}

func TestCovNewPusherDefaultsLoggerWhenNil(t *testing.T) {
	p := NewPusher("https://pam.example.com", "cpk", uuid.New(), 3*time.Second, nil)
	if p.logger == nil {
		t.Fatal("logger = nil, want a default logger")
	}
	if p.client.Timeout != 3*time.Second {
		t.Errorf("client timeout = %v, want 3s", p.client.Timeout)
	}
	if p.serverURL != "https://pam.example.com" || p.apiKey != "cpk" {
		t.Fatalf("pusher fields not set: %+v", p)
	}
}

func TestCovRegisterSendsSignedRequestAndDecodesResponse(t *testing.T) {
	wantAgent := uuid.New()
	pusher, agentID, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(domain.AgentRegisterResponse{
			AgentID:        wantAgent,
			ApprovalStatus: domain.AgentApprovalPending,
			Message:        "awaiting approval",
		})
	})

	resp, err := pusher.Register(context.Background(), "agent-1", 4242, "v1.2.3", "host-a")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	paths, methods, auths, types := rec.snapshot()
	if paths[0] != "/api/v1/discovery/agents/register" {
		t.Errorf("path = %q", paths[0])
	}
	if methods[0] != http.MethodPost {
		t.Errorf("method = %q, want POST", methods[0])
	}
	if auths[0] != "Bearer cpk_secret" {
		t.Errorf("Authorization = %q, want Bearer cpk_secret", auths[0])
	}
	if types[0] != "application/json" {
		t.Errorf("Content-Type = %q", types[0])
	}

	var sent domain.AgentRegisterRequest
	if err := json.Unmarshal(rec.body(0), &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.AgentID != agentID {
		t.Errorf("sent agent_id = %s, want %s", sent.AgentID, agentID)
	}
	if sent.Name != "agent-1" || sent.AccountID != 4242 || sent.Version != "v1.2.3" || sent.Hostname != "host-a" {
		t.Fatalf("sent register payload = %+v", sent)
	}

	if resp.AgentID != wantAgent {
		t.Errorf("resp.AgentID = %s, want %s", resp.AgentID, wantAgent)
	}
	if resp.ApprovalStatus != domain.AgentApprovalPending {
		t.Errorf("resp.ApprovalStatus = %q", resp.ApprovalStatus)
	}
	if resp.Message != "awaiting approval" {
		t.Errorf("resp.Message = %q", resp.Message)
	}
}

func TestCovRegisterSurfacesServerRejection(t *testing.T) {
	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "forbidden",
			"detail": "agent not approved",
		})
	})

	resp, err := pusher.Register(context.Background(), "agent-1", 1, "v1", "host")
	if err == nil {
		t.Fatal("Register() error = nil, want rejection error")
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
	for _, want := range []string{"registration failed (status 403)", "forbidden", "agent not approved"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCovRegisterFailsOnUndecodableSuccessBody(t *testing.T) {
	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})

	if _, err := pusher.Register(context.Background(), "agent-1", 1, "v1", "host"); err == nil ||
		!strings.Contains(err.Error(), "decode response") {
		t.Fatalf("Register() error = %v, want decode response", err)
	}
}

func TestCovRegisterFailsWhenServerUnreachable(t *testing.T) {
	if _, err := covDeadServerPusher(t).Register(context.Background(), "agent-1", 1, "v1", "host"); err == nil ||
		!strings.Contains(err.Error(), "http request") {
		t.Fatalf("Register() error = %v, want http request failure", err)
	}
}

func TestCovPushResourcesSendsIngestPayload(t *testing.T) {
	jobID := uuid.New()
	syncJobID := uuid.New()
	pusher, agentID, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.IngestResponse{
			JobID:            jobID,
			ResourcesFound:   2,
			ResourcesCreated: 1,
			ResourcesUpdated: 1,
		})
	})

	resources := []domain.DiscoveredResource{
		{ID: uuid.New(), AccountID: 9, Provider: "aws", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-1", CIDR: "10.0.0.0/16"},
		{ID: uuid.New(), AccountID: 9, Provider: "aws", ResourceType: domain.ResourceTypeSubnet, ResourceID: "subnet-1", CIDR: "10.0.1.0/24"},
	}

	if err := pusher.PushResources(context.Background(), 9, resources, &syncJobID, 2, time.Millisecond); err != nil {
		t.Fatalf("PushResources() error = %v", err)
	}

	if rec.count() != 1 {
		t.Fatalf("requests = %d, want exactly 1 on success", rec.count())
	}
	paths, methods, auths, _ := rec.snapshot()
	if paths[0] != "/api/v1/discovery/ingest" {
		t.Errorf("path = %q", paths[0])
	}
	if methods[0] != http.MethodPost {
		t.Errorf("method = %q, want POST", methods[0])
	}
	if auths[0] != "Bearer cpk_secret" {
		t.Errorf("Authorization = %q", auths[0])
	}

	var sent domain.IngestRequest
	if err := json.Unmarshal(rec.body(0), &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.AccountID != 9 {
		t.Errorf("account_id = %d, want 9", sent.AccountID)
	}
	if sent.AgentID == nil || *sent.AgentID != agentID {
		t.Errorf("agent_id = %v, want %s", sent.AgentID, agentID)
	}
	if sent.SyncJobID == nil || *sent.SyncJobID != syncJobID {
		t.Errorf("sync_job_id = %v, want %s", sent.SyncJobID, syncJobID)
	}
	if len(sent.Resources) != 2 {
		t.Fatalf("resources = %d, want 2", len(sent.Resources))
	}
	if sent.Resources[0].ResourceID != "vpc-1" || sent.Resources[1].ResourceID != "subnet-1" {
		t.Errorf("resource ids = %q, %q", sent.Resources[0].ResourceID, sent.Resources[1].ResourceID)
	}
}

func TestCovPushResourcesOmitsSyncJobIDWhenNil(t *testing.T) {
	pusher, _, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.IngestResponse{})
	})

	if err := pusher.PushResources(context.Background(), 1, nil, nil, 0, time.Millisecond); err != nil {
		t.Fatalf("PushResources() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.body(0), &raw); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if _, ok := raw["sync_job_id"]; ok {
		t.Errorf("sync_job_id present in payload %v, want it omitted when nil", raw)
	}
}

func TestCovPushResourcesDoesNotRetryClientErrors(t *testing.T) {
	pusher, _, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "invalid_request",
			"detail": "unknown account",
		})
	})

	err := pusher.PushResources(context.Background(), 1, nil, nil, 5, time.Millisecond)
	if err == nil {
		t.Fatal("PushResources() error = nil, want rejection")
	}
	if rec.count() != 1 {
		t.Fatalf("requests = %d, want 1 (4xx must not be retried)", rec.count())
	}
	for _, want := range []string{"server rejected request (status 400)", "invalid_request", "unknown account"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCovPushResourcesRetriesServerErrorsThenFails(t *testing.T) {
	pusher, _, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	err := pusher.PushResources(context.Background(), 1, nil, nil, 2, time.Millisecond)
	if err == nil {
		t.Fatal("PushResources() error = nil, want failure after retries")
	}
	if rec.count() != 3 {
		t.Fatalf("requests = %d, want 3 (initial attempt plus 2 retries)", rec.count())
	}
	if !strings.Contains(err.Error(), "push failed after 3 attempts") {
		t.Errorf("error = %q, want attempt count", err.Error())
	}
	if !strings.Contains(err.Error(), "server error (status 502)") {
		t.Errorf("error = %q, want the wrapped last error", err.Error())
	}
}

func TestCovPushResourcesSucceedsOnRetryAfterServerError(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	pusher, _, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(domain.IngestResponse{ResourcesFound: 1})
	})

	if err := pusher.PushResources(context.Background(), 1, nil, nil, 3, time.Millisecond); err != nil {
		t.Fatalf("PushResources() error = %v, want success on the retry", err)
	}
	if rec.count() != 2 {
		t.Fatalf("requests = %d, want 2", rec.count())
	}
}

func TestCovPushResourcesAbortsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.WriteHeader(http.StatusInternalServerError)
	})

	// A long backoff guarantees the cancelled context wins the retry select.
	err := pusher.PushResources(ctx, 1, nil, nil, 5, 30*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PushResources() error = %v, want context.Canceled", err)
	}
}

func TestCovPushResourcesFailsOnUndecodableSuccessBody(t *testing.T) {
	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})

	if err := pusher.PushResources(context.Background(), 1, nil, nil, 2, time.Millisecond); err == nil ||
		!strings.Contains(err.Error(), "decode response") {
		t.Fatalf("PushResources() error = %v, want decode response", err)
	}
}

func TestCovPushResourcesRetriesTransportFailures(t *testing.T) {
	err := covDeadServerPusher(t).PushResources(context.Background(), 1, nil, nil, 1, time.Millisecond)
	if err == nil {
		t.Fatal("PushResources() error = nil, want transport failure")
	}
	if !strings.Contains(err.Error(), "push failed after 2 attempts") {
		t.Errorf("error = %q, want 2 attempts", err.Error())
	}
	if !strings.Contains(err.Error(), "http request") {
		t.Errorf("error = %q, want the wrapped transport error", err.Error())
	}
}

func TestCovPushOrgResourcesKeepsExplicitAgentID(t *testing.T) {
	pusher, agentID, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.BulkIngestResponse{
			AccountsProcessed: 2,
			AccountsCreated:   1,
			TotalResources:    5,
			Errors:            []string{"account 3 skipped"},
		})
	})

	explicit := uuid.New().String()
	err := pusher.PushOrgResources(context.Background(), domain.BulkIngestRequest{
		AgentID:  explicit,
		Accounts: []domain.OrgAccountIngest{{AWSAccountID: "123456789012", Provider: "aws"}},
	}, 0, time.Millisecond)
	if err != nil {
		t.Fatalf("PushOrgResources() error = %v", err)
	}

	var sent domain.BulkIngestRequest
	if err := json.Unmarshal(rec.body(0), &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.AgentID != explicit {
		t.Errorf("agent_id = %q, want the caller-supplied %q", sent.AgentID, explicit)
	}
	if sent.AgentID == agentID.String() {
		t.Error("caller-supplied agent_id was overwritten with the pusher's own id")
	}

	paths, _, auths, _ := rec.snapshot()
	if paths[0] != "/api/v1/discovery/ingest/org" {
		t.Errorf("path = %q", paths[0])
	}
	if auths[0] != "Bearer cpk_secret" {
		t.Errorf("Authorization = %q", auths[0])
	}
}

func TestCovPushOrgResourcesDoesNotRetryClientErrors(t *testing.T) {
	pusher, _, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_accounts", "detail": "empty list"})
	})

	err := pusher.PushOrgResources(context.Background(), domain.BulkIngestRequest{}, 4, time.Millisecond)
	if err == nil {
		t.Fatal("PushOrgResources() error = nil, want rejection")
	}
	if rec.count() != 1 {
		t.Fatalf("requests = %d, want 1 (4xx must not be retried)", rec.count())
	}
	for _, want := range []string{"server rejected request (status 422)", "bad_accounts", "empty list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCovPushOrgResourcesRetriesServerErrors(t *testing.T) {
	pusher, _, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	err := pusher.PushOrgResources(context.Background(), domain.BulkIngestRequest{}, 1, time.Millisecond)
	if err == nil {
		t.Fatal("PushOrgResources() error = nil, want failure after retries")
	}
	if rec.count() != 2 {
		t.Fatalf("requests = %d, want 2", rec.count())
	}
	if !strings.Contains(err.Error(), "org push failed after 2 attempts") {
		t.Errorf("error = %q", err.Error())
	}
	if !strings.Contains(err.Error(), "server error (status 503)") {
		t.Errorf("error = %q, want the wrapped last error", err.Error())
	}
}

func TestCovPushOrgResourcesAbortsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.WriteHeader(http.StatusInternalServerError)
	})

	err := pusher.PushOrgResources(ctx, domain.BulkIngestRequest{}, 5, 30*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PushOrgResources() error = %v, want context.Canceled", err)
	}
}

func TestCovPushOrgResourcesFailsOnUndecodableSuccessBody(t *testing.T) {
	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>"))
	})

	if err := pusher.PushOrgResources(context.Background(), domain.BulkIngestRequest{}, 0, time.Millisecond); err == nil ||
		!strings.Contains(err.Error(), "decode response") {
		t.Fatalf("PushOrgResources() error = %v, want decode response", err)
	}
}

func TestCovPushOrgResourcesRetriesTransportFailures(t *testing.T) {
	err := covDeadServerPusher(t).PushOrgResources(context.Background(), domain.BulkIngestRequest{}, 1, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "org push failed after 2 attempts") {
		t.Fatalf("PushOrgResources() error = %v, want transport failure after 2 attempts", err)
	}
}

func TestCovHeartbeatSendsIdentityAndReturnsSyncJob(t *testing.T) {
	syncJobID := uuid.New()
	pusher, agentID, rec := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.AgentHeartbeatResponse{
			Status:    "ok",
			SyncJobID: &syncJobID,
			AccountID: 77,
		})
	})

	resp, err := pusher.Heartbeat(context.Background(), "agent-1", 77, "v9", "host-b")
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}

	paths, methods, auths, types := rec.snapshot()
	if paths[0] != "/api/v1/discovery/agents/heartbeat" {
		t.Errorf("path = %q", paths[0])
	}
	if methods[0] != http.MethodPost {
		t.Errorf("method = %q, want POST", methods[0])
	}
	if auths[0] != "Bearer cpk_secret" || types[0] != "application/json" {
		t.Errorf("headers = %q / %q", auths[0], types[0])
	}

	var sent domain.AgentHeartbeatRequest
	if err := json.Unmarshal(rec.body(0), &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.AgentID != agentID || sent.Name != "agent-1" || sent.AccountID != 77 || sent.Version != "v9" || sent.Hostname != "host-b" {
		t.Fatalf("sent heartbeat payload = %+v", sent)
	}

	if resp.Status != "ok" {
		t.Errorf("resp.Status = %q", resp.Status)
	}
	if resp.SyncJobID == nil || *resp.SyncJobID != syncJobID {
		t.Errorf("resp.SyncJobID = %v, want %s", resp.SyncJobID, syncJobID)
	}
	if resp.AccountID != 77 {
		t.Errorf("resp.AccountID = %d, want 77", resp.AccountID)
	}
}

func TestCovHeartbeatSurfacesServerError(t *testing.T) {
	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized", "detail": "bad key"})
	})

	resp, err := pusher.Heartbeat(context.Background(), "agent-1", 1, "v1", "host")
	if err == nil {
		t.Fatal("Heartbeat() error = nil, want failure")
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
	for _, want := range []string{"heartbeat failed (status 401)", "unauthorized", "bad key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCovHeartbeatFailsOnUndecodableSuccessBody(t *testing.T) {
	pusher, _, _ := covNewPusherFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("nope"))
	})

	if _, err := pusher.Heartbeat(context.Background(), "a", 1, "v1", "host"); err == nil ||
		!strings.Contains(err.Error(), "decode response") {
		t.Fatalf("Heartbeat() error = %v, want decode response", err)
	}
}

func TestCovHeartbeatFailsWhenServerUnreachable(t *testing.T) {
	if _, err := covDeadServerPusher(t).Heartbeat(context.Background(), "a", 1, "v1", "host"); err == nil ||
		!strings.Contains(err.Error(), "http request") {
		t.Fatalf("Heartbeat() error = %v, want http request failure", err)
	}
}
