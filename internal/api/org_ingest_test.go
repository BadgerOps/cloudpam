package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func TestTriggerSyncQueuesHealthyAgent(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()

	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key:      "aws:123456789012",
		Name:     "prod",
		Provider: "aws",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	agentID := uuid.New()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID:         agentID,
		Name:       "org-agent",
		AccountID:  1,
		APIKeyID:   "key-1",
		Version:    "1.0.0",
		Hostname:   "host-1",
		LastSeenAt: time.Now().UTC(),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync",
		`{"account_id":`+itoa(account.ID)+`}`, http.StatusOK)
	var job domain.SyncJob
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.Status != domain.SyncJobStatusPending || job.Source != "agent" {
		t.Fatalf("job = %+v, want pending agent job", job)
	}
	if job.AgentID == nil || *job.AgentID != agentID {
		t.Fatalf("AgentID = %v, want %s", job.AgentID, agentID)
	}

	claimed, err := ds.ClaimPendingAgentSync(t.Context(), agentID)
	if err != nil {
		t.Fatalf("claim pending sync: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed job = %s, want %s", claimed.ID, job.ID)
	}
	if claimed.AccountID != account.ID {
		t.Fatalf("AccountID = %d, want %d", claimed.AccountID, account.ID)
	}
	if claimed.Status != domain.SyncJobStatusRunning || claimed.StartedAt == nil {
		t.Fatalf("claimed job = %+v, want running with started_at", claimed)
	}
}

func TestTriggerSyncAgentUsesRequestedAccountWhenStoredAccountMissing(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()

	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key:      "aws:123456789012",
		Name:     "prod",
		Provider: "aws",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	agentID := uuid.New()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID:         agentID,
		Name:       "org-agent",
		AccountID:  125672604241,
		APIKeyID:   "key-1",
		LastSeenAt: time.Now().UTC(),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync",
		`{"agent_id":"`+agentID.String()+`","account_id":`+itoa(account.ID)+`}`, http.StatusOK)
	var job domain.SyncJob
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.AccountID != account.ID {
		t.Fatalf("AccountID = %d, want %d", job.AccountID, account.ID)
	}
	if job.AgentID == nil || *job.AgentID != agentID {
		t.Fatalf("AgentID = %v, want %s", job.AgentID, agentID)
	}
}

func TestTriggerSyncAgentResolvesAWSAccountKeyFallback(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()

	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key:      "aws:125672604241",
		Name:     "management",
		Provider: "aws",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	agentID := uuid.New()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID:         agentID,
		Name:       "org-agent",
		AccountID:  125672604241,
		APIKeyID:   "key-1",
		LastSeenAt: time.Now().UTC(),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync",
		`{"agent_id":"`+agentID.String()+`"}`, http.StatusOK)
	var job domain.SyncJob
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.AccountID != account.ID {
		t.Fatalf("AccountID = %d, want %d", job.AccountID, account.ID)
	}
}

func TestOrgIngestRefreshesRegisteredAgentLastSeen(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()

	agentID := uuid.New()
	oldSeen := time.Now().UTC().Add(-30 * time.Minute)
	createdAt := oldSeen.Add(-time.Hour)
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID:             agentID,
		Name:           "org-agent",
		AccountID:      1,
		APIKeyID:       "key-1",
		ApprovalStatus: domain.AgentApprovalApproved,
		Version:        "1.0.0",
		Hostname:       "host-1",
		LastSeenAt:     oldSeen,
		CreatedAt:      createdAt,
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	body := "{" +
		`"agent_id":"` + agentID.String() + `",` +
		`"accounts":[{` +
		`"aws_account_id":"123456789012",` +
		`"account_name":"prod",` +
		`"provider":"aws",` +
		`"regions":["us-east-1"],` +
		`"resources":[]` +
		`}]` +
		"}"
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body, http.StatusOK)

	agent, err := ds.GetAgent(t.Context(), agentID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if !agent.LastSeenAt.After(oldSeen) {
		t.Fatalf("LastSeenAt = %s, want after %s", agent.LastSeenAt, oldSeen)
	}
	if agent.Name != "org-agent" || agent.AccountID != 1 || agent.APIKeyID != "key-1" || agent.Version != "1.0.0" || agent.Hostname != "host-1" {
		t.Fatalf("agent metadata was not preserved: %+v", agent)
	}
	if !agent.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s, want %s", agent.CreatedAt, createdAt)
	}
}

func TestOrgIngestRejectsInvalidAgentID(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	body := `{"agent_id":"not-a-uuid","accounts":[{"aws_account_id":"123456789012","account_name":"prod","provider":"aws"}]}`
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body, http.StatusBadRequest)
}
