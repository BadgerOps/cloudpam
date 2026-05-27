package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

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
