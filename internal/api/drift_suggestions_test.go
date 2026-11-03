package api

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func addDiscovered(t *testing.T, ds *storage.MemoryDiscoveryStore, accountID int64, typ domain.CloudResourceType, providerID, cidr string, poolID *int64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	if err := ds.UpsertDiscoveredResource(context.Background(), domain.DiscoveredResource{
		ID:           id,
		AccountID:    accountID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: typ,
		ResourceID:   providerID,
		Name:         providerID,
		CIDR:         cidr,
		PoolID:       poolID,
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	}); err != nil {
		t.Fatalf("upsert resource %s: %v", providerID, err)
	}
	return id
}

func addPool(t *testing.T, ms *storage.MemoryStore, in domain.CreatePool) domain.Pool {
	t.Helper()
	p, err := ms.CreatePool(context.Background(), in)
	if err != nil {
		t.Fatalf("create pool %s: %v", in.CIDR, err)
	}
	return p
}

func detectDrift(t *testing.T, mux *stdhttp.ServeMux) []domain.DriftItem {
	t.Helper()
	rr := doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/detect", `{}`, stdhttp.StatusOK)
	var resp domain.RunDriftDetectionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.Items
}

func itemForResource(t *testing.T, items []domain.DriftItem, resourceID uuid.UUID) domain.DriftItem {
	t.Helper()
	for _, item := range items {
		if item.ResourceID != nil && *item.ResourceID == resourceID {
			return item
		}
	}
	t.Fatalf("no drift item for resource %s in %+v", resourceID, items)
	return domain.DriftItem{}
}

func assertSuggestion(t *testing.T, item domain.DriftItem, action domain.DriftAction, poolID *int64) {
	t.Helper()
	if item.SuggestedAction != action {
		t.Fatalf("%s: suggested_action = %q (%s), want %q", item.Title, item.SuggestedAction, item.SuggestionReason, action)
	}
	switch {
	case poolID == nil && item.SuggestedPoolID != nil:
		t.Fatalf("%s: suggested_pool_id = %d, want none", item.Title, *item.SuggestedPoolID)
	case poolID != nil && (item.SuggestedPoolID == nil || *item.SuggestedPoolID != *poolID):
		t.Fatalf("%s: suggested_pool_id = %v, want %d", item.Title, item.SuggestedPoolID, *poolID)
	}
	if item.SuggestionReason == "" {
		t.Fatalf("%s: suggestion_reason is empty", item.Title)
	}
}

func TestDriftSuggestions_LinkToExactMatchThenApply(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:link", Name: "Link", Provider: "aws"})
	pool := addPool(t, ms, domain.CreatePool{Name: "prod", CIDR: "10.0.0.0/16", AccountID: &acct.ID})
	resID := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-link", "10.0.0.0/16", nil)

	item := itemForResource(t, detectDrift(t, mux), resID)
	assertSuggestion(t, item, domain.DriftActionLinkPool, &pool.ID)

	doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/discovery/resources/"+resID.String()+"/link",
		fmt.Sprintf(`{"pool_id":%d}`, *item.SuggestedPoolID), stdhttp.StatusOK)

	if items := detectDrift(t, mux); len(items) != 0 {
		t.Fatalf("following the link suggestion left drift: %+v", items)
	}
}

func TestDriftSuggestions_CreateUnderContainingParentThenApply(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:create", Name: "Create", Provider: "aws"})
	top := addPool(t, ms, domain.CreatePool{Name: "org", CIDR: "10.0.0.0/8"})
	region := addPool(t, ms, domain.CreatePool{Name: "region", CIDR: "10.1.0.0/16", ParentID: &top.ID})
	resID := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-new", "10.1.4.0/22", nil)

	item := itemForResource(t, detectDrift(t, mux), resID)
	assertSuggestion(t, item, domain.DriftActionCreatePool, &region.ID)

	rr := doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/discovery/import/apply",
		fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s"]}`, acct.ID, resID), stdhttp.StatusOK)
	var applied domain.DiscoveryImportApplyResponse
	if err := json.NewDecoder(rr.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if len(applied.CreatedPoolIDs) != 1 {
		t.Fatalf("import created %d pools, want 1: %+v", len(applied.CreatedPoolIDs), applied)
	}
	created, _, _ := ms.GetPool(ctx, applied.CreatedPoolIDs[0])
	if created.ParentID == nil || *created.ParentID != region.ID {
		t.Fatalf("created pool parent = %v, want suggested parent %d", created.ParentID, region.ID)
	}

	if items := detectDrift(t, mux); len(items) != 0 {
		t.Fatalf("following the create suggestion left drift: %+v", items)
	}
}

func TestDriftSuggestions_UnmanagedWithoutCandidates(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:none", Name: "None", Provider: "aws"})
	other, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:other", Name: "Other", Provider: "aws"})
	addPool(t, ms, domain.CreatePool{Name: "other-acct", CIDR: "192.168.0.0/16", AccountID: &other.ID})
	addPool(t, ms, domain.CreatePool{Name: "small", CIDR: "172.16.1.0/24"})

	topLevel := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-top", "10.9.0.0/16", nil)
	crossAccount := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-cross", "192.168.4.0/24", nil)
	overlapping := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-overlap", "172.16.0.0/16", nil)
	eni := addDiscovered(t, ds, acct.ID, domain.ResourceTypeNetworkInterface, "eni-1", "10.9.0.5/32", nil)

	items := detectDrift(t, mux)
	assertSuggestion(t, itemForResource(t, items, topLevel), domain.DriftActionCreatePool, nil)
	assertSuggestion(t, itemForResource(t, items, crossAccount), domain.DriftActionReview, nil)
	assertSuggestion(t, itemForResource(t, items, overlapping), domain.DriftActionReview, nil)
	assertSuggestion(t, itemForResource(t, items, eni), domain.DriftActionReview, nil)
}

func TestDriftSuggestions_CIDRMismatchRelinksToMatchingPool(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:mismatch", Name: "Mismatch", Provider: "aws"})
	stale := addPool(t, ms, domain.CreatePool{Name: "old", CIDR: "10.2.0.0/16", AccountID: &acct.ID})
	match := addPool(t, ms, domain.CreatePool{Name: "new", CIDR: "10.3.0.0/16", AccountID: &acct.ID})
	movedID := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-moved", "10.3.0.0/16", &stale.ID)

	orphanless := addPool(t, ms, domain.CreatePool{Name: "lonely", CIDR: "10.4.0.0/16", AccountID: &acct.ID})
	unmatchedID := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-unmatched", "10.5.0.0/16", &orphanless.ID)

	items := detectDrift(t, mux)
	assertSuggestion(t, itemForResource(t, items, movedID), domain.DriftActionLinkPool, &match.ID)
	assertSuggestion(t, itemForResource(t, items, unmatchedID), domain.DriftActionReview, nil)

	doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/discovery/resources/"+movedID.String()+"/link",
		fmt.Sprintf(`{"pool_id":%d}`, match.ID), stdhttp.StatusOK)
	for _, item := range detectDrift(t, mux) {
		if item.ResourceID != nil && *item.ResourceID == movedID {
			t.Fatalf("relinking left drift for the moved resource: %+v", item)
		}
	}
}

func TestDriftSuggestions_OrphanedPoolDeprecation(t *testing.T) {
	mux, ms, _, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:orphan", Name: "Orphan", Provider: "aws"})
	pool := addPool(t, ms, domain.CreatePool{Name: "gone", CIDR: "10.6.0.0/16", AccountID: &acct.ID, Source: domain.PoolSourceDiscovered})

	items := detectDrift(t, mux)
	if len(items) != 1 || items[0].Type != domain.DriftTypeOrphanedPool {
		t.Fatalf("expected one orphaned pool item, got %+v", items)
	}
	assertSuggestion(t, items[0], domain.DriftActionDeprecatePool, &pool.ID)

	deprecated := domain.PoolStatusDeprecated
	if _, _, err := ms.UpdatePool(ctx, pool.ID, domain.UpdatePool{AccountID: &acct.ID, Status: &deprecated}); err != nil {
		t.Fatal(err)
	}
	rr := doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift/"+items[0].ID, "", stdhttp.StatusOK)
	var item domain.DriftItem
	if err := json.NewDecoder(rr.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	assertSuggestion(t, item, domain.DriftActionReview, nil)
}

func TestDriftSuggestions_OnlyOpenItemsCarryOne(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:closed", Name: "Closed", Provider: "aws"})
	addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-closed", "10.7.0.0/16", nil)

	items := detectDrift(t, mux)
	rr := doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/"+items[0].ID+"/resolve", `{}`, stdhttp.StatusOK)
	var resolved domain.DriftItem
	if err := json.NewDecoder(rr.Body).Decode(&resolved); err != nil {
		t.Fatal(err)
	}
	rr = doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift/"+items[0].ID, "", stdhttp.StatusOK)
	if err := json.NewDecoder(rr.Body).Decode(&resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.SuggestedAction != "" || resolved.SuggestedPoolID != nil {
		t.Fatalf("resolved item carries a suggestion: %+v", resolved)
	}
}

func getDriftItem(t *testing.T, mux *stdhttp.ServeMux, id string) domain.DriftItem {
	t.Helper()
	rr := doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift/"+id, "", stdhttp.StatusOK)
	var item domain.DriftItem
	if err := json.NewDecoder(rr.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestDriftSuggestions_SubnetIsPreviewedAloneLikeApply(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:pair", Name: "Pair", Provider: "aws"})
	vpcID := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-pair", "10.8.0.0/16", nil)
	subnet, err := ds.GetDiscoveredResource(ctx, addDiscovered(t, ds, acct.ID, domain.ResourceTypeSubnet, "subnet-pair", "10.8.1.0/24", nil))
	if err != nil {
		t.Fatal(err)
	}
	parent := "vpc-pair"
	subnet.ParentResourceID = &parent
	if err := ds.UpsertDiscoveredResource(ctx, *subnet); err != nil {
		t.Fatal(err)
	}

	items := detectDrift(t, mux)
	assertSuggestion(t, itemForResource(t, items, vpcID), domain.DriftActionCreatePool, nil)
	listed := itemForResource(t, items, subnet.ID)
	assertSuggestion(t, listed, domain.DriftActionReview, nil)
	if detail := getDriftItem(t, mux, listed.ID); detail.SuggestedAction != listed.SuggestedAction || detail.SuggestionReason != listed.SuggestionReason {
		t.Fatalf("detail suggestion %q (%s) differs from list %q (%s)", detail.SuggestedAction, detail.SuggestionReason, listed.SuggestedAction, listed.SuggestionReason)
	}

	rr := doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/discovery/import/apply",
		fmt.Sprintf(`{"account_id":%d,"resource_ids":["%s"]}`, acct.ID, subnet.ID), stdhttp.StatusOK)
	var applied domain.DiscoveryImportApplyResponse
	if err := json.NewDecoder(rr.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if applied.PoolsCreated != 0 {
		t.Fatalf("single-subnet apply created %d pools, so review was the wrong suggestion", applied.PoolsCreated)
	}
}

func TestDriftSuggestions_RelinkFollowsCurrentResourceCIDR(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:moving", Name: "Moving", Provider: "aws"})
	linked := addPool(t, ms, domain.CreatePool{Name: "linked", CIDR: "10.2.0.0/16", AccountID: &acct.ID})
	addPool(t, ms, domain.CreatePool{Name: "first", CIDR: "10.3.0.0/16", AccountID: &acct.ID})
	second := addPool(t, ms, domain.CreatePool{Name: "second", CIDR: "10.4.0.0/16", AccountID: &acct.ID})
	resID := addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-moving", "10.3.0.0/16", &linked.ID)
	item := itemForResource(t, detectDrift(t, mux), resID)

	setCIDR := func(cidr string) {
		t.Helper()
		res, err := ds.GetDiscoveredResource(ctx, resID)
		if err != nil {
			t.Fatal(err)
		}
		res.CIDR = cidr
		if err := ds.UpsertDiscoveredResource(ctx, *res); err != nil {
			t.Fatal(err)
		}
	}

	setCIDR("10.4.0.0/16")
	assertSuggestion(t, getDriftItem(t, mux, item.ID), domain.DriftActionLinkPool, &second.ID)

	setCIDR("10.2.0.0/16")
	assertSuggestion(t, getDriftItem(t, mux, item.ID), domain.DriftActionReview, nil)

	setCIDR("10.3.0.0/16")
	if err := ds.UnlinkResource(ctx, resID); err != nil {
		t.Fatal(err)
	}
	assertSuggestion(t, getDriftItem(t, mux, item.ID), domain.DriftActionReview, nil)
}

func TestDriftSuggestions_OrphanRelinkedAfterDetectionIsNotDeprecated(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:back", Name: "Back", Provider: "aws"})
	pool := addPool(t, ms, domain.CreatePool{Name: "back", CIDR: "10.6.0.0/16", AccountID: &acct.ID, Source: domain.PoolSourceDiscovered})

	items := detectDrift(t, mux)
	if len(items) != 1 || items[0].Type != domain.DriftTypeOrphanedPool {
		t.Fatalf("expected one orphaned pool item, got %+v", items)
	}
	addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, "vpc-back", "10.6.0.0/16", &pool.ID)
	assertSuggestion(t, getDriftItem(t, mux, items[0].ID), domain.DriftActionReview, nil)
}
