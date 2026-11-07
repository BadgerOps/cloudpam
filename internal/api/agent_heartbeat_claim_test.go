package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
)

func TestHeartbeatWithClaimSyncFalseLeavesThePendingJob(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	key := &auth.APIKey{ID: "key-abc", Name: "agent"}
	agentID := uuid.New()
	job, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{
		ID: uuid.New(), AccountID: 4, Status: domain.SyncJobStatusPending,
		Source: "agent", AgentID: &agentID, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}
	heartbeat := func(extra string) domain.AgentHeartbeatResponse {
		t.Helper()
		body := `{"agent_id":"` + agentID.String() + `","name":"edge","account_id":4` + extra + `}`
		rr := assertStatusCov(t, doReqWithKeyCov(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/agents/heartbeat", body, key), http.StatusOK)
		var resp domain.AgentHeartbeatResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp
	}

	if resp := heartbeat(`,"claim_sync":false`); resp.SyncJobID != nil {
		t.Fatalf("busy heartbeat claimed job %s", resp.SyncJobID)
	}
	got, err := ds.GetSyncJob(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if got.Status != domain.SyncJobStatusPending {
		t.Fatalf("job status after busy heartbeat = %q, want pending", got.Status)
	}

	if resp := heartbeat(""); resp.SyncJobID == nil || *resp.SyncJobID != job.ID {
		t.Fatalf("heartbeat without claim_sync = %+v, want job %s", resp, job.ID)
	}
}
