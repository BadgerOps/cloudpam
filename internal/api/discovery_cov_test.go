package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
)

// --- shared coverage-suite helpers (suffix "Cov" to avoid symbol collisions) ---

// doReqCov issues a request against a mux and returns the recorder without
// asserting on the status code.
func doReqCov(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// doReqWithKeyCov issues a request with an API key placed directly on the
// request context (used for handlers registered without auth middleware that
// still read the authenticated principal).
func doReqWithKeyCov(t *testing.T, mux *http.ServeMux, method, path, body string, key *auth.APIKey) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != nil {
		req = req.WithContext(auth.ContextWithAPIKey(req.Context(), key))
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// decodeErrCov decodes an apiError body.
func decodeErrCov(t *testing.T, rr *httptest.ResponseRecorder) apiError {
	t.Helper()
	var e apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode error body %q: %v", rr.Body.String(), err)
	}
	return e
}

// assertStatusCov checks the status code and returns the recorder.
func assertStatusCov(t *testing.T, rr *httptest.ResponseRecorder, want int) *httptest.ResponseRecorder {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rr.Code, want, rr.Body.String())
	}
	return rr
}

// --- agents endpoints ---

func TestDiscoveryAgentsListCov(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()

	// empty list still returns a JSON array, never null
	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/agents", ""), http.StatusOK)
	var empty domain.DiscoveryAgentsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &empty); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("expected empty non-nil items, got %#v", empty.Items)
	}

	now := time.Now().UTC()
	healthy := uuid.New()
	stale := uuid.New()
	offline := uuid.New()
	seeds := []struct {
		id       uuid.UUID
		lastSeen time.Time
		want     domain.AgentStatus
	}{
		{healthy, now.Add(-time.Minute), domain.AgentStatusHealthy},
		{stale, now.Add(-10 * time.Minute), domain.AgentStatusStale},
		{offline, now.Add(-30 * time.Minute), domain.AgentStatusOffline},
	}
	for _, s := range seeds {
		if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
			ID: s.id, Name: "agent-" + s.id.String()[:4], AccountID: 7,
			APIKeyID: "k", LastSeenAt: s.lastSeen, CreatedAt: now,
		}); err != nil {
			t.Fatalf("upsert agent: %v", err)
		}
	}

	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/agents", ""), http.StatusOK)
	var resp domain.DiscoveryAgentsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(resp.Items))
	}
	got := map[uuid.UUID]domain.AgentStatus{}
	for _, a := range resp.Items {
		got[a.ID] = a.Status
	}
	for _, s := range seeds {
		if got[s.id] != s.want {
			t.Fatalf("agent %s status = %q, want %q", s.id, got[s.id], s.want)
		}
	}

	// account_id filter narrows the result set
	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/agents?account_id=999", ""), http.StatusOK)
	var filtered domain.DiscoveryAgentsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(filtered.Items) != 0 {
		t.Fatalf("expected no agents for account 999, got %d", len(filtered.Items))
	}
}

func TestDiscoveryAgentsListMethodNotAllowedCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()
	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPut, "/api/v1/discovery/agents", ""), http.StatusMethodNotAllowed)
	if allow := rr.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow header = %q, want GET", allow)
	}
	if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestDiscoveryGetAgentCov(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	id := uuid.New()
	now := time.Now().UTC()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID: id, Name: "edge", AccountID: 3, APIKeyID: "k1",
		LastSeenAt: now.Add(-8 * time.Minute), CreatedAt: now,
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{"invalid uuid", http.MethodGet, "/api/v1/discovery/agents/not-a-uuid", http.StatusBadRequest, "invalid agent id"},
		{"unknown agent", http.MethodGet, "/api/v1/discovery/agents/" + uuid.New().String(), http.StatusNotFound, "agent not found"},
		{"wrong method", http.MethodPatch, "/api/v1/discovery/agents/" + id.String(), http.StatusMethodNotAllowed, "method not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, tc.method, tc.path, ""), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/agents/"+id.String(), ""), http.StatusOK)
	var agent domain.DiscoveryAgent
	if err := json.Unmarshal(rr.Body.Bytes(), &agent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if agent.ID != id || agent.Name != "edge" {
		t.Fatalf("unexpected agent: %+v", agent)
	}
	if agent.Status != domain.AgentStatusStale {
		t.Fatalf("status = %q, want stale", agent.Status)
	}
}

func TestDiscoveryDeleteAgentCov(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	id := uuid.New()
	now := time.Now().UTC()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{ID: id, Name: "gone", AccountID: 1, LastSeenAt: now, CreatedAt: now}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodDelete, "/api/v1/discovery/agents/nope", ""), http.StatusBadRequest)
	if e := decodeErrCov(t, rr); e.Error != "invalid agent id" {
		t.Fatalf("error = %q", e.Error)
	}

	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodDelete, "/api/v1/discovery/agents/"+uuid.New().String(), ""), http.StatusNotFound)
	if e := decodeErrCov(t, rr); e.Error != "agent not found" {
		t.Fatalf("error = %q", e.Error)
	}

	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodDelete, "/api/v1/discovery/agents/"+id.String(), ""), http.StatusOK)
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "deleted" {
		t.Fatalf("status = %q, want deleted", body["status"])
	}
	if _, err := ds.GetAgent(t.Context(), id); err == nil {
		t.Fatal("expected agent to be removed from the store")
	}
}

func TestDiscoveryAgentHeartbeatCov(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	key := &auth.APIKey{ID: "key-abc", Name: "agent"}
	agentID := uuid.New()

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/agents/heartbeat", ""), http.StatusMethodNotAllowed)
		if got := rr.Header().Get("Allow"); got != http.MethodPost {
			t.Fatalf("Allow = %q", got)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/agents/heartbeat", `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/agents/heartbeat", `{"name":"a"}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "agent_id, name, and account_id are required" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	body := `{"agent_id":"` + agentID.String() + `","name":"edge","account_id":4,"version":"1.2.3","hostname":"h1"}`

	t.Run("unauthenticated", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/agents/heartbeat", body), http.StatusUnauthorized)
		if e := decodeErrCov(t, rr); e.Error != "api key required" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("upserts agent", func(t *testing.T) {
		rr := assertStatusCov(t, doReqWithKeyCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/agents/heartbeat", body, key), http.StatusOK)
		var resp domain.AgentHeartbeatResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Status != "ok" || resp.SyncJobID != nil {
			t.Fatalf("unexpected response: %+v", resp)
		}
		agent, err := ds.GetAgent(t.Context(), agentID)
		if err != nil {
			t.Fatalf("get agent: %v", err)
		}
		if agent.APIKeyID != "key-abc" || agent.Version != "1.2.3" || agent.Hostname != "h1" {
			t.Fatalf("agent not persisted from heartbeat: %+v", agent)
		}
	})

	t.Run("claims pending sync job", func(t *testing.T) {
		job, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{
			ID: uuid.New(), AccountID: 4, Status: domain.SyncJobStatusPending,
			Source: "agent", AgentID: &agentID, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create sync job: %v", err)
		}
		rr := assertStatusCov(t, doReqWithKeyCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/agents/heartbeat", body, key), http.StatusOK)
		var resp domain.AgentHeartbeatResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.SyncJobID == nil || *resp.SyncJobID != job.ID {
			t.Fatalf("expected claimed job %s, got %+v", job.ID, resp)
		}
		if resp.AccountID != 4 {
			t.Fatalf("account_id = %d, want 4", resp.AccountID)
		}
	})
}

// --- sync endpoints ---

func TestDiscoverySyncListJobsCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{"missing account_id", "/api/v1/discovery/sync"},
		{"non numeric account_id", "/api/v1/discovery/sync?account_id=abc"},
		{"zero account_id", "/api/v1/discovery/sync?account_id=0"},
		{"negative account_id", "/api/v1/discovery/sync?account_id=-3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, tc.path, ""), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != "account_id is required and must be a positive integer" {
				t.Fatalf("error = %q", e.Error)
			}
		})
	}

	// no jobs → empty (non-nil) list
	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/sync?account_id=1", ""), http.StatusOK)
	var empty domain.SyncJobsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &empty); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("expected empty non-nil items, got %#v", empty.Items)
	}

	for i := 0; i < 3; i++ {
		if _, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{
			ID: uuid.New(), AccountID: acct.ID, Status: domain.SyncJobStatusCompleted,
			Source: "local", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("create sync job: %v", err)
		}
	}

	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/sync?account_id=1", ""), http.StatusOK)
	var all domain.SyncJobsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &all); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(all.Items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(all.Items))
	}

	// limit clamps the returned page; limit <= 0 falls back to the default
	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/sync?account_id=1&limit=2", ""), http.StatusOK)
	var limited domain.SyncJobsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &limited); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(limited.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(limited.Items))
	}

	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/sync?account_id=1&limit=0", ""), http.StatusOK)
	var defaulted domain.SyncJobsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &defaulted); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(defaulted.Items) != 3 {
		t.Fatalf("len(items) = %d, want 3 with default limit", len(defaulted.Items))
	}
}

func TestDiscoverySyncMethodNotAllowedCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()
	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodDelete, "/api/v1/discovery/sync", ""), http.StatusMethodNotAllowed)
	if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow = %q", allow)
	}
}

func TestDiscoverySyncJobByIDCov(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	job, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{
		ID: uuid.New(), AccountID: 1, Status: domain.SyncJobStatusRunning,
		Source: "local", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{"invalid id", http.MethodGet, "/api/v1/discovery/sync/xyz", http.StatusBadRequest, "invalid sync job id"},
		{"unknown id", http.MethodGet, "/api/v1/discovery/sync/" + uuid.New().String(), http.StatusNotFound, "sync job not found"},
		{"wrong method", http.MethodPost, "/api/v1/discovery/sync/" + job.ID.String(), http.StatusMethodNotAllowed, "method not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, tc.method, tc.path, ""), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/sync/"+job.ID.String(), ""), http.StatusOK)
	var got domain.SyncJob
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != job.ID || got.Status != domain.SyncJobStatusRunning {
		t.Fatalf("unexpected job: %+v", got)
	}
}

func TestDiscoveryTriggerSyncErrorsCov(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()

	offline := uuid.New()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID: offline, Name: "old", AccountID: 1,
		LastSeenAt: time.Now().UTC().Add(-time.Hour), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"malformed json", `{`, http.StatusBadRequest, "invalid request body"},
		{"missing account", `{}`, http.StatusBadRequest, "account_id is required and must be a positive integer"},
		{"negative account", `{"account_id":-1}`, http.StatusBadRequest, "account_id is required and must be a positive integer"},
		{"unknown account", `{"account_id":42}`, http.StatusNotFound, "account not found"},
		{"invalid agent id", `{"agent_id":"not-a-uuid"}`, http.StatusBadRequest, "invalid agent_id"},
		{"unknown agent", `{"agent_id":"` + uuid.New().String() + `"}`, http.StatusNotFound, "agent not found"},
		{"unhealthy agent", `{"agent_id":"` + offline.String() + `"}`, http.StatusConflict, "agent is not healthy"},
		{"all agents but none healthy", `{"all_agents":true}`, http.StatusConflict, "no healthy discovery agents are connected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync", tc.body), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}
}

func TestDiscoveryTriggerSyncAgentAccountResolutionCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:999", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Agent's AccountID is the raw AWS account number, which is resolved via
	// the "aws:<id>" account key fallback.
	agentID := uuid.New()
	now := time.Now().UTC()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID: agentID, Name: "aws-agent", AccountID: 999, LastSeenAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync",
		`{"agent_id":"`+agentID.String()+`"}`), http.StatusOK)
	var job domain.SyncJob
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.AccountID != acct.ID {
		t.Fatalf("account_id = %d, want %d (resolved via aws: key)", job.AccountID, acct.ID)
	}
	if job.Status != domain.SyncJobStatusPending || job.Source != "agent" || job.AgentID == nil || *job.AgentID != agentID {
		t.Fatalf("unexpected job: %+v", job)
	}

	// Explicit account_id that does not exist → 404
	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync",
		`{"agent_id":"`+agentID.String()+`","account_id":777}`), http.StatusNotFound)
	if e := decodeErrCov(t, rr); e.Error != "agent account not found" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestDiscoveryTriggerSyncPrefersConnectedAgentCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:5", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	agentID := uuid.New()
	now := time.Now().UTC()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID: agentID, Name: "match", AccountID: acct.ID, LastSeenAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync",
		`{"account_id":`+itoa(acct.ID)+`}`), http.StatusOK)
	var job domain.SyncJob
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Source != "agent" || job.AgentID == nil || *job.AgentID != agentID {
		t.Fatalf("expected job dispatched to connected agent, got %+v", job)
	}
	if job.Status != domain.SyncJobStatusPending {
		t.Fatalf("status = %q, want pending", job.Status)
	}
}

// --- resources endpoints ---

func TestDiscoveryResourcesListValidationCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/resources", `{}`), http.StatusMethodNotAllowed)
	if allow := rr.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q", allow)
	}

	for _, path := range []string{
		"/api/v1/discovery/resources",
		"/api/v1/discovery/resources?account_id=",
		"/api/v1/discovery/resources?account_id=zero",
		"/api/v1/discovery/resources?account_id=0",
	} {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, path, ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "account_id is required and must be a positive integer" {
			t.Fatalf("%s: error = %q", path, e.Error)
		}
	}
}

func TestDiscoveryResourcesPaginationDefaultsCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
			ID: uuid.New(), AccountID: acct.ID, Provider: "aws", Region: "us-east-1",
			ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-" + itoa(int64(i)),
			Name: "vpc", CIDR: "10." + itoa(int64(i)) + ".0.0/16",
			Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now,
		}); err != nil {
			t.Fatalf("upsert resource: %v", err)
		}
	}

	tests := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
		wantItems    int
	}{
		{"defaults", "", 1, 50, 3},
		{"page zero normalised", "&page=0&page_size=0", 1, 50, 3},
		{"negative page normalised", "&page=-5", 1, 50, 3},
		{"explicit page size", "&page=1&page_size=2", 1, 2, 2},
		{"page past the end", "&page=9&page_size=2", 9, 2, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet,
				"/api/v1/discovery/resources?account_id="+itoa(acct.ID)+tc.query, ""), http.StatusOK)
			var resp domain.DiscoveryResourcesResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Page != tc.wantPage || resp.PageSize != tc.wantPageSize {
				t.Fatalf("page/page_size = %d/%d, want %d/%d", resp.Page, resp.PageSize, tc.wantPage, tc.wantPageSize)
			}
			if resp.Total != 3 {
				t.Fatalf("total = %d, want 3", resp.Total)
			}
			if resp.Items == nil {
				t.Fatal("items must never be null")
			}
			if len(resp.Items) != tc.wantItems {
				t.Fatalf("len(items) = %d, want %d", len(resp.Items), tc.wantItems)
			}
		})
	}
}

func TestDiscoveryResourcesLinkedFilterCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "p", CIDR: "10.0.0.0/16", AccountID: &acct.ID})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	now := time.Now().UTC()
	linked := uuid.New()
	unlinked := uuid.New()
	for _, id := range []uuid.UUID{linked, unlinked} {
		if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
			ID: id, AccountID: acct.ID, Provider: "aws", Region: "us-east-1",
			ResourceType: domain.ResourceTypeVPC, ResourceID: id.String(), Name: "vpc",
			CIDR: "10.0.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now,
		}); err != nil {
			t.Fatalf("upsert resource: %v", err)
		}
	}
	if err := ds.LinkResourceToPool(t.Context(), linked, pool.ID); err != nil {
		t.Fatalf("link: %v", err)
	}

	cases := map[string]uuid.UUID{"true": linked, "1": linked, "false": unlinked, "0": unlinked}
	for q, want := range cases {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet,
			"/api/v1/discovery/resources?account_id="+itoa(acct.ID)+"&linked="+q, ""), http.StatusOK)
		var resp domain.DiscoveryResourcesResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Items) != 1 || resp.Items[0].ID != want {
			t.Fatalf("linked=%s returned %d items (want exactly %s)", q, len(resp.Items), want)
		}
	}
}

func TestDiscoveryResourceByIDCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	id := uuid.New()
	now := time.Now().UTC()
	if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
		ID: id, AccountID: acct.ID, Provider: "aws", Region: "eu-west-1",
		ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-x", Name: "vpc-x",
		CIDR: "172.16.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("upsert resource: %v", err)
	}

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{"invalid id", http.MethodGet, "/api/v1/discovery/resources/abc", http.StatusBadRequest, "invalid resource id"},
		{"invalid link id", http.MethodPost, "/api/v1/discovery/resources/abc/link", http.StatusBadRequest, "invalid resource id"},
		{"unknown id", http.MethodGet, "/api/v1/discovery/resources/" + uuid.New().String(), http.StatusNotFound, "resource not found"},
		{"wrong method", http.MethodPut, "/api/v1/discovery/resources/" + id.String(), http.StatusMethodNotAllowed, "method not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, tc.method, tc.path, ""), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/resources/"+id.String(), ""), http.StatusOK)
	var res domain.DiscoveredResource
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.ID != id || res.CIDR != "172.16.0.0/16" {
		t.Fatalf("unexpected resource: %+v", res)
	}
}

func TestDiscoveryLinkCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acctA, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account a: %v", err)
	}
	acctB, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:2", Name: "b", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account b: %v", err)
	}
	poolA, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "pa", CIDR: "10.0.0.0/16", AccountID: &acctA.ID})
	if err != nil {
		t.Fatalf("create pool a: %v", err)
	}
	poolB, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "pb", CIDR: "10.1.0.0/16", AccountID: &acctB.ID})
	if err != nil {
		t.Fatalf("create pool b: %v", err)
	}

	now := time.Now().UTC()
	active := uuid.New()
	stale := uuid.New()
	if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
		ID: active, AccountID: acctA.ID, Provider: "aws", Region: "us-east-1",
		ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-a", Name: "vpc-a",
		CIDR: "10.0.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("upsert active: %v", err)
	}
	if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
		ID: stale, AccountID: acctA.ID, Provider: "aws", Region: "us-east-1",
		ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-stale", Name: "vpc-stale",
		CIDR: "10.2.0.0/16", Status: domain.DiscoveryStatusStale, DiscoveredAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("upsert stale: %v", err)
	}

	linkPath := func(id uuid.UUID) string { return "/api/v1/discovery/resources/" + id.String() + "/link" }

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPut, linkPath(active), ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "POST, DELETE" {
			t.Fatalf("Allow = %q", allow)
		}
	})

	badBodies := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"missing pool_id", `{}`},
		{"negative pool_id", `{"pool_id":-2}`},
	}
	for _, tc := range badBodies {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, linkPath(active), tc.body), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != "pool_id is required and must be a positive integer" {
				t.Fatalf("error = %q", e.Error)
			}
		})
	}

	t.Run("resource not found", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, linkPath(uuid.New()),
			`{"pool_id":`+itoa(poolA.ID)+`}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "resource not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("pool not found", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, linkPath(active), `{"pool_id":9999}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "pool not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("cross account rejected", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, linkPath(active),
			`{"pool_id":`+itoa(poolB.ID)+`}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "pool account does not match discovered resource account" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("stale resource rejected", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, linkPath(stale),
			`{"pool_id":`+itoa(poolA.ID)+`}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error == "" {
			t.Fatal("expected an error message for stale resource link")
		}
		res, err := ds.GetDiscoveredResource(t.Context(), stale)
		if err != nil {
			t.Fatalf("get stale: %v", err)
		}
		if res.PoolID != nil {
			t.Fatal("stale resource must not be linked")
		}
	})

	t.Run("link then unlink", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, linkPath(active),
			`{"pool_id":`+itoa(poolA.ID)+`}`), http.StatusOK)
		var body map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body["status"] != "linked" {
			t.Fatalf("status = %q", body["status"])
		}
		res, err := ds.GetDiscoveredResource(t.Context(), active)
		if err != nil {
			t.Fatalf("get resource: %v", err)
		}
		if res.PoolID == nil || *res.PoolID != poolA.ID {
			t.Fatalf("resource not linked: %+v", res)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodDelete, linkPath(active), ""), http.StatusOK)
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body["status"] != "unlinked" {
			t.Fatalf("status = %q", body["status"])
		}
		res, err = ds.GetDiscoveredResource(t.Context(), active)
		if err != nil {
			t.Fatalf("get resource: %v", err)
		}
		if res.PoolID != nil {
			t.Fatalf("resource still linked: %+v", res)
		}
	})

	t.Run("unlink unknown resource", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodDelete, linkPath(uuid.New()), ""), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "resource not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})
}

// --- ingest endpoints ---

func TestDiscoveryIngestCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	errCases := []struct {
		name     string
		method   string
		body     string
		wantCode int
		wantErr  string
	}{
		{"method not allowed", http.MethodGet, "", http.StatusMethodNotAllowed, "method not allowed"},
		{"malformed json", http.MethodPost, `{`, http.StatusBadRequest, "invalid request body"},
		{"missing account", http.MethodPost, `{}`, http.StatusBadRequest, "account_id is required and must be a positive integer"},
		{"unknown account", http.MethodPost, `{"account_id":404}`, http.StatusNotFound, "account not found"},
		{"unknown sync job", http.MethodPost, `{"account_id":1,"sync_job_id":"` + uuid.New().String() + `"}`, http.StatusBadRequest, "sync job not found"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, tc.method, "/api/v1/discovery/ingest", tc.body), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	t.Run("creates job and resources", func(t *testing.T) {
		body := `{"account_id":` + itoa(acct.ID) + `,"resources":[{"provider":"aws","region":"us-east-1","resource_type":"vpc","resource_id":"vpc-1","name":"v","cidr":"10.0.0.0/16","status":"active"}]}`
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest", body), http.StatusOK)
		var resp domain.IngestResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.ResourcesFound != 1 || resp.ResourcesCreated != 1 {
			t.Fatalf("unexpected ingest response: %+v", resp)
		}
		job, err := ds.GetSyncJob(t.Context(), resp.JobID)
		if err != nil {
			t.Fatalf("get sync job: %v", err)
		}
		if job.Status != domain.SyncJobStatusCompleted || job.Source != "agent" {
			t.Fatalf("unexpected job: %+v", job)
		}
	})

	t.Run("continues an existing sync job", func(t *testing.T) {
		agentID := uuid.New()
		existing, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{
			ID: uuid.New(), AccountID: acct.ID, Status: domain.SyncJobStatusPending,
			Source: "agent", AgentID: &agentID, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create sync job: %v", err)
		}
		body := `{"account_id":` + itoa(acct.ID) + `,"sync_job_id":"` + existing.ID.String() + `","agent_id":"` + agentID.String() + `","resources":[]}`
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest", body), http.StatusOK)
		var resp domain.IngestResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.JobID != existing.ID {
			t.Fatalf("job_id = %s, want the supplied job %s", resp.JobID, existing.ID)
		}
		job, err := ds.GetSyncJob(t.Context(), existing.ID)
		if err != nil {
			t.Fatalf("get sync job: %v", err)
		}
		if job.Status != domain.SyncJobStatusCompleted {
			t.Fatalf("job status = %q, want completed", job.Status)
		}
		if job.StartedAt == nil || job.CompletedAt == nil {
			t.Fatalf("expected started/completed timestamps, got %+v", job)
		}
	})
}

func TestDiscoveryOrgIngestCov(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()

	errCases := []struct {
		name     string
		method   string
		body     string
		wantCode int
		wantErr  string
	}{
		{"method not allowed", http.MethodGet, "", http.StatusMethodNotAllowed, "method not allowed"},
		{"malformed json", http.MethodPost, `{`, http.StatusBadRequest, "invalid request body"},
		{"no accounts", http.MethodPost, `{"accounts":[]}`, http.StatusBadRequest, "accounts list is required"},
		{"unknown sync job", http.MethodPost, `{"accounts":[{"aws_account_id":"1"}],"sync_job_id":"` + uuid.New().String() + `"}`, http.StatusBadRequest, "sync job not found"},
		{"invalid agent id", http.MethodPost, `{"accounts":[{"aws_account_id":"1"}],"agent_id":"nope"}`, http.StatusBadRequest, "invalid agent_id"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, tc.method, "/api/v1/discovery/ingest/org", tc.body), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	t.Run("auto creates accounts and records errors", func(t *testing.T) {
		body := `{"accounts":[
			{"aws_account_id":"111111111111","account_name":"prod","regions":["us-east-1"],
			 "resources":[{"provider":"aws","region":"us-east-1","resource_type":"vpc","resource_id":"vpc-1","name":"v","cidr":"10.0.0.0/16","status":"active"}]},
			{"aws_account_id":""}
		]}`
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body), http.StatusOK)
		var resp domain.BulkIngestResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.AccountsCreated != 1 || resp.AccountsProcessed != 1 || resp.TotalResources != 1 {
			t.Fatalf("unexpected response: %+v", resp)
		}
		if len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0], "empty aws_account_id") {
			t.Fatalf("expected an empty-account error, got %#v", resp.Errors)
		}
		acct, err := st.GetAccountByKey(t.Context(), "aws:111111111111")
		if err != nil {
			t.Fatalf("get auto created account: %v", err)
		}
		if acct.Name != "prod" || acct.ExternalID != "111111111111" {
			t.Fatalf("unexpected auto-created account: %+v", acct)
		}
	})

	t.Run("marks supplied sync job failed when errors occur", func(t *testing.T) {
		job, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{
			ID: uuid.New(), AccountID: 1, Status: domain.SyncJobStatusPending,
			Source: "agent", CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create sync job: %v", err)
		}
		body := `{"accounts":[{"aws_account_id":""}],"sync_job_id":"` + job.ID.String() + `"}`
		assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body), http.StatusOK)

		got, err := ds.GetSyncJob(t.Context(), job.ID)
		if err != nil {
			t.Fatalf("get sync job: %v", err)
		}
		if got.Status != domain.SyncJobStatusFailed {
			t.Fatalf("status = %q, want failed", got.Status)
		}
		if !strings.Contains(got.ErrorMessage, "empty aws_account_id") {
			t.Fatalf("error_message = %q", got.ErrorMessage)
		}
	})

	t.Run("refreshes agent heartbeat", func(t *testing.T) {
		agentID := uuid.New()
		old := time.Now().UTC().Add(-2 * time.Hour)
		if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
			ID: agentID, Name: "org-agent", AccountID: 1, LastSeenAt: old, CreatedAt: old,
		}); err != nil {
			t.Fatalf("upsert agent: %v", err)
		}
		body := `{"accounts":[{"aws_account_id":"222222222222"}],"agent_id":"` + agentID.String() + `"}`
		assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body), http.StatusOK)

		agent, err := ds.GetAgent(t.Context(), agentID)
		if err != nil {
			t.Fatalf("get agent: %v", err)
		}
		if !agent.LastSeenAt.After(old) {
			t.Fatalf("last_seen_at not refreshed: %v", agent.LastSeenAt)
		}
	})

	t.Run("unknown agent id is tolerated", func(t *testing.T) {
		body := `{"accounts":[{"aws_account_id":"333333333333"}],"agent_id":"` + uuid.New().String() + `"}`
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body), http.StatusOK)
		var resp domain.BulkIngestResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.AccountsProcessed != 1 {
			t.Fatalf("accounts_processed = %d, want 1", resp.AccountsProcessed)
		}
	})
}

// --- import subroutes ---

func TestDiscoveryImportSubroutesCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/bogus", `{}`), http.StatusNotFound)
	if e := decodeErrCov(t, rr); e.Error != "not found" {
		t.Fatalf("error = %q", e.Error)
	}

	for _, path := range []string{"/api/v1/discovery/import/preview", "/api/v1/discovery/import/apply"} {
		t.Run(path+" method", func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
			if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
				t.Fatalf("Allow = %q", allow)
			}
		})
		t.Run(path+" malformed json", func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{`), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
				t.Fatalf("error = %q", e.Error)
			}
		})
		t.Run(path+" invalid selection", func(t *testing.T) {
			for _, body := range []string{`{}`, `{"account_id":0,"resource_ids":["` + uuid.New().String() + `"]}`, `{"account_id":1,"resource_ids":[]}`} {
				rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, body), http.StatusBadRequest)
				if e := decodeErrCov(t, rr); e.Error == "" {
					t.Fatalf("expected a validation error for body %s", body)
				}
			}
		})
	}
}

func TestDiscoveryImportSchemaValidationCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/discovery/import", ""), http.StatusMethodNotAllowed)
	if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("Allow = %q", allow)
	}

	for _, body := range []string{`{`, `{"account_id":0}`, `{"account_id":-1}`} {
		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import", body), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "account_id is required and must be a positive integer" {
			t.Fatalf("body %s: error = %q", body, e.Error)
		}
	}

	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import", `{"account_id":4242}`), http.StatusNotFound)
	if e := decodeErrCov(t, rr); e.Error != "account not found" {
		t.Fatalf("error = %q", e.Error)
	}
}
