package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/blocks?page_size=all", "", http.StatusOK)
	var blocks struct {
		Items []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Status   string `json:"status"`
			Source   string `json:"source"`
			CIDR     string `json:"cidr"`
			ParentID *int64 `json:"parent_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &blocks); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	if len(blocks.Items) != 2 {
		t.Fatalf("len(blocks) = %d, want imported vpc and subnet", len(blocks.Items))
	}
	var sawRootVPC, sawSubnet bool
	for _, block := range blocks.Items {
		if block.CIDR == "10.0.0.0/16" && block.Type == "vpc" && block.Source == "discovered" && block.ParentID == nil {
			sawRootVPC = true
		}
		if block.CIDR == "10.0.1.0/24" && block.Type == "subnet" && block.Status == "active" && block.ParentID != nil {
			sawSubnet = true
		}
	}
	if !sawRootVPC || !sawSubnet {
		t.Fatalf("discovered blocks missing from allocated blocks: %+v", blocks.Items)
	}
}

func TestDiscoveryImportPreviewAndApplySelectedResources(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	vpcID := uuid.New()
	subnetID := uuid.New()
	parent := "vpc-1"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpcID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-1",
		Name:         "prod-vpc",
		CIDR:         "10.10.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "prod-subnet",
		CIDR:             "10.10.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s","%s"]}`, account.ID, vpcID, subnetID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	var preview domain.DiscoveryImportPreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &preview); err != nil {
		t.Fatalf("unmarshal preview: %v", err)
	}
	if preview.Importable != 2 || preview.Blocked != 0 {
		t.Fatalf("unexpected preview counts: %+v", preview)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/apply", body, http.StatusOK)
	var applied domain.DiscoveryImportApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &applied); err != nil {
		t.Fatalf("unmarshal apply: %v", err)
	}
	if applied.PoolsCreated != 2 || applied.ResourcesLinked != 2 || applied.Skipped != 0 {
		t.Fatalf("unexpected apply response: %+v", applied)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("len(pools) = %d, want 2", len(pools))
	}
}

func TestDiscoveryImportPreviewBlocksMissingParent(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	subnetID := uuid.New()
	parent := "vpc-missing"
	now := time.Now().UTC()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "orphan-subnet",
		CIDR:             "10.20.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s"]}`, account.ID, subnetID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	item := singleDiscoveryPreviewItem(t, rr)
	if item.Status != "blocked" || !containsString(item.Issues, "missing_parent") {
		t.Fatalf("unexpected preview item: %+v", item)
	}
}

func TestDiscoveryImportPreviewBlocksOutsideSelectedPool(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod", CIDR: "10.30.0.0/16", Type: domain.PoolTypeSupernet})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	vpcID := uuid.New()
	now := time.Now().UTC()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpcID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-1",
		Name:         "outside-vpc",
		CIDR:         "10.31.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})

	body := fmt.Sprintf(`{"account_id":%d,"pool_id":%d,"resource_ids":["%s"]}`, account.ID, pool.ID, vpcID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	item := singleDiscoveryPreviewItem(t, rr)
	if item.Status != "blocked" || !containsString(item.Issues, "outside_pool") {
		t.Fatalf("unexpected preview item: %+v", item)
	}
}

func TestDiscoveryImportPreviewFlagsDuplicateAcrossAccounts(t *testing.T) {
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
	vpc1ID := uuid.New()
	vpc2ID := uuid.New()
	for _, res := range []domain.DiscoveredResource{
		{ID: vpc1ID, AccountID: a1.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-prod", Name: "prod", CIDR: "10.40.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
		{ID: vpc2ID, AccountID: a2.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-dev", Name: "dev", CIDR: "10.40.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
	} {
		upsertDiscoveredForImportTest(t, ds, res)
	}

	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s"]}`, a1.ID, vpc1ID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	item := singleDiscoveryPreviewItem(t, rr)
	if item.Status != "conflict" || !containsString(item.Issues, "duplicate_cidr") || len(item.DuplicateResourceIDs) != 1 {
		t.Fatalf("unexpected preview item: %+v", item)
	}
}

func TestDiscoveryImportPreviewKeepsEIPAsNetworkObject(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	eipID := uuid.New()
	now := time.Now().UTC()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           eipID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeElasticIP,
		ResourceID:   "eipalloc-1",
		Name:         "prod-eip",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})

	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s"]}`, account.ID, eipID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	item := singleDiscoveryPreviewItem(t, rr)
	if item.Status != "linked_only" || item.ProposedManagedType != "network_object" || !containsString(item.Issues, "network_object_only") {
		t.Fatalf("unexpected preview item: %+v", item)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/apply", body, http.StatusOK)
	var applied domain.DiscoveryImportApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &applied); err != nil {
		t.Fatalf("unmarshal apply: %v", err)
	}
	if applied.PoolsCreated != 0 || applied.ResourcesLinked != 0 || applied.Skipped != 1 {
		t.Fatalf("EIP should not be imported as a pool: %+v", applied)
	}
}

func upsertDiscoveredForImportTest(t *testing.T, ds interface {
	UpsertDiscoveredResource(context.Context, domain.DiscoveredResource) error
}, res domain.DiscoveredResource) {
	t.Helper()
	if err := ds.UpsertDiscoveredResource(t.Context(), res); err != nil {
		t.Fatalf("upsert discovered resource: %v", err)
	}
}

func singleDiscoveryPreviewItem(t *testing.T, rr *httptest.ResponseRecorder) domain.DiscoveryImportPreviewItem {
	t.Helper()
	var preview domain.DiscoveryImportPreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &preview); err != nil {
		t.Fatalf("unmarshal preview: %v", err)
	}
	if len(preview.Items) != 1 {
		t.Fatalf("len(preview.items) = %d, want 1: %+v", len(preview.Items), preview)
	}
	return preview.Items[0]
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
