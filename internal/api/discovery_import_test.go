package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
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

func TestDiscoveryImportPreviewFindsParentBeyondFirstPage(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	for i := 0; i < 1001; i++ {
		upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
			ID:           uuid.New(),
			AccountID:    account.ID,
			Provider:     "aws",
			Region:       "us-east-1",
			ResourceType: domain.ResourceTypeElasticIP,
			ResourceID:   fmt.Sprintf("eipalloc-page-%04d", i),
			Name:         fmt.Sprintf("page filler %04d", i),
			CIDR:         fmt.Sprintf("198.51.%d.%d/32", i/255, i%255),
			Status:       domain.DiscoveryStatusActive,
			DiscoveredAt: now.Add(time.Duration(i) * time.Second),
			LastSeenAt:   now.Add(time.Duration(i) * time.Second),
		})
	}

	vpcID := uuid.New()
	subnetID := uuid.New()
	parent := "vpc-paged"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpcID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   parent,
		Name:         "paged-vpc",
		CIDR:         "10.50.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now.Add(-2 * time.Hour),
		LastSeenAt:   now,
	})
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-paged",
		Name:             "paged-subnet",
		CIDR:             "10.50.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now.Add(-1 * time.Hour),
		LastSeenAt:       now,
	})

	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s","%s"]}`, account.ID, vpcID, subnetID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	var preview domain.DiscoveryImportPreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &preview); err != nil {
		t.Fatalf("unmarshal preview: %v", err)
	}
	if preview.Importable != 2 || preview.Blocked != 0 {
		t.Fatalf("expected paged parent lookup to make both resources importable: %+v", preview)
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

func TestDiscoveryImportPreviewAndApplyRejectInvalidSelectedPool(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	otherAccount, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:222222222222", Name: "dev", Provider: "aws"})
	if err != nil {
		t.Fatalf("create other account: %v", err)
	}
	otherPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "dev", CIDR: "10.32.0.0/16", Type: domain.PoolTypeSupernet, AccountID: &otherAccount.ID})
	if err != nil {
		t.Fatalf("create other pool: %v", err)
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
		Name:         "prod-vpc",
		CIDR:         "10.32.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})

	missingPoolBody := fmt.Sprintf(`{"account_id":%d,"pool_id":999999,"resource_ids":["%s"]}`, account.ID, vpcID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", missingPoolBody, http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/apply", missingPoolBody, http.StatusBadRequest)

	accountMismatchBody := fmt.Sprintf(`{"account_id":%d,"pool_id":%d,"resource_ids":["%s"]}`, account.ID, otherPool.ID, vpcID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", accountMismatchBody, http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/apply", accountMismatchBody, http.StatusBadRequest)
}

func TestDiscoveryImportApplyDeletesCreatedPoolWhenLinkFails(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	vpcID := uuid.New()
	now := time.Now().UTC()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpcID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-link-fails",
		Name:         "vpc-link-fails",
		CIDR:         "10.33.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})
	discSrv.store = &failLinkDiscoveryStore{DiscoveryStore: ds, failID: vpcID}

	body := fmt.Sprintf("{\"account_id\":%d,\"resource_ids\":[\"%s\"]}", account.ID, vpcID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/apply", body, http.StatusOK)
	var applied domain.DiscoveryImportApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &applied); err != nil {
		t.Fatalf("unmarshal apply: %v", err)
	}
	if applied.PoolsCreated != 0 || applied.ResourcesLinked != 0 || applied.Skipped != 1 {
		t.Fatalf("failed link should skip without completed imports: %+v", applied)
	}
	if len(applied.CreatedPoolIDs) != 0 {
		t.Fatalf("created pool id should be removed after cleanup succeeds: %+v", applied.CreatedPoolIDs)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("link failure should clean up created pool, got %+v", pools)
	}
	res, err := ds.GetDiscoveredResource(t.Context(), vpcID)
	if err != nil {
		t.Fatalf("get discovered resource: %v", err)
	}
	if res.PoolID != nil {
		t.Fatalf("link failure should leave resource unlinked, got pool %d", *res.PoolID)
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

func TestDiscoveryImportPreviewFindsDuplicateBeyondFirstPage(t *testing.T) {
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
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpc1ID,
		AccountID:    a1.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-prod",
		Name:         "prod",
		CIDR:         "10.60.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})
	for i := 0; i < 1001; i++ {
		upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
			ID:           uuid.New(),
			AccountID:    a2.ID,
			Provider:     "aws",
			Region:       "us-east-1",
			ResourceType: domain.ResourceTypeElasticIP,
			ResourceID:   fmt.Sprintf("eipalloc-dup-page-%04d", i),
			Name:         fmt.Sprintf("duplicate filler %04d", i),
			CIDR:         fmt.Sprintf("203.0.%d.%d/32", i/255, i%255),
			Status:       domain.DiscoveryStatusActive,
			DiscoveredAt: now.Add(time.Duration(i) * time.Second),
			LastSeenAt:   now.Add(time.Duration(i) * time.Second),
		})
	}
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpc2ID,
		AccountID:    a2.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-dev",
		Name:         "dev",
		CIDR:         "10.60.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now.Add(-time.Hour),
		LastSeenAt:   now,
	})

	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s"]}`, a1.ID, vpc1ID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/import/preview", body, http.StatusOK)
	item := singleDiscoveryPreviewItem(t, rr)
	if item.Status != "conflict" || !containsString(item.Issues, "duplicate_cidr") || len(item.DuplicateResourceIDs) != 1 {
		t.Fatalf("expected paged duplicate lookup to find conflict: %+v", item)
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

type failLinkDiscoveryStore struct {
	storage.DiscoveryStore
	failID uuid.UUID
}

func (s *failLinkDiscoveryStore) LinkResourceToPool(ctx context.Context, resourceID uuid.UUID, poolID int64) error {
	if resourceID == s.failID {
		return errors.New("forced link failure")
	}
	return s.DiscoveryStore.LinkResourceToPool(ctx, resourceID, poolID)
}
