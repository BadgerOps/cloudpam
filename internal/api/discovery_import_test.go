package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func TestTriggerSyncAllHealthyAgents(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	a1, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:111111111111", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account 1: %v", err)
	}
	a2, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:222222222222", Name: "dev", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account 2: %v", err)
	}

	now := time.Now().UTC()
	for _, account := range []domain.Account{a1, a2} {
		if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
			ID:         uuid.New(),
			Name:       account.Name + "-agent",
			AccountID:  account.ID,
			APIKeyID:   "key-" + account.Key,
			LastSeenAt: now,
			CreatedAt:  now,
		}); err != nil {
			t.Fatalf("upsert agent: %v", err)
		}
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/sync", `{"all_agents":true}`, http.StatusOK)
	var resp domain.SyncJobsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(resp.Items))
	}
	for _, job := range resp.Items {
		if job.Status != domain.SyncJobStatusPending || job.Source != "agent" || job.AgentID == nil {
			t.Fatalf("unexpected job: %+v", job)
		}
	}
}

func TestImportDiscoveredSchemaCreatesAndLinksPools(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	vpcID := uuid.New()
	subnetID := uuid.New()
	parent := "vpc-1"
	if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
		ID:           vpcID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-1",
		Name:         "prod-vpc",
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	}); err != nil {
		t.Fatalf("upsert vpc: %v", err)
	}
	if err := ds.UpsertDiscoveredResource(t.Context(), domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "prod-subnet",
		CIDR:             "10.0.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	}); err != nil {
		t.Fatalf("upsert subnet: %v", err)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import", `{"account_id":1}`, http.StatusOK)
	var resp discoveryImportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.PoolsCreated != 2 || resp.ResourcesLinked != 2 {
		t.Fatalf("unexpected import response: %+v", resp)
	}

	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("len(pools) = %d, want 2", len(pools))
	}

	vpc, err := ds.GetDiscoveredResource(t.Context(), vpcID)
	if err != nil {
		t.Fatalf("get vpc: %v", err)
	}
	subnet, err := ds.GetDiscoveredResource(t.Context(), subnetID)
	if err != nil {
		t.Fatalf("get subnet: %v", err)
	}
	if vpc.PoolID == nil || subnet.PoolID == nil {
		t.Fatalf("resources were not linked: vpc=%v subnet=%v", vpc.PoolID, subnet.PoolID)
	}
}
