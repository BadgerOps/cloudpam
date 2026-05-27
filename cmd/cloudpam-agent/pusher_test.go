package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func TestPushOrgResourcesIncludesAgentID(t *testing.T) {
	agentID := uuid.New()
	var got domain.BulkIngestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/discovery/ingest/org" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(domain.BulkIngestResponse{AccountsProcessed: 1})
	}))
	defer srv.Close()

	pusher := NewPusher(srv.URL, "test-key", agentID, time.Second, slog.Default())
	err := pusher.PushOrgResources(t.Context(), domain.BulkIngestRequest{
		Accounts: []domain.OrgAccountIngest{{
			AWSAccountID: "123456789012",
			AccountName:  "prod",
			Provider:     "aws",
		}},
	}, 0, time.Millisecond)
	if err != nil {
		t.Fatalf("PushOrgResources() error: %v", err)
	}

	if got.AgentID != agentID.String() {
		t.Fatalf("AgentID = %q, want %q", got.AgentID, agentID.String())
	}
}
