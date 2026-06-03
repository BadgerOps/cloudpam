package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestNetworkFlatShowsDiscoveredObjectsAndDuplicateConflict(t *testing.T) {
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
	eipID := uuid.New()
	for _, res := range []domain.DiscoveredResource{
		{ID: vpc1ID, AccountID: a1.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-prod", Name: "prod-vpc", CIDR: "10.70.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
		{ID: vpc2ID, AccountID: a2.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-dev", Name: "dev-vpc", CIDR: "10.70.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
		{ID: eipID, AccountID: a1.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeElasticIP, ResourceID: "eipalloc-1", Name: "prod-eip", CIDR: "198.51.100.10/32", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now, Metadata: map[string]string{"public_ip": "198.51.100.10"}},
	} {
		upsertDiscoveredForImportTest(t, ds, res)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/flat", "", http.StatusOK)
	var resp domain.NetworkViewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal flat: %v", err)
	}
	if resp.Total != 3 {
		t.Fatalf("total = %d, want 3", resp.Total)
	}
	if resp.ConflictCount != 1 {
		t.Fatalf("conflict_count = %d, want 1: %+v", resp.ConflictCount, resp.Conflicts)
	}
	var sawEIP bool
	for _, item := range resp.Items {
		if item.ObjectType == "elastic_ip" && item.IPAddress == "198.51.100.10" {
			sawEIP = true
		}
	}
	if !sawEIP {
		t.Fatalf("flat view did not include EIP network object: %+v", resp.Items)
	}
}

func TestNetworkObjectsCreateUpdateAndAppearInMergedView(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	body := fmt.Sprintf(`{"object_type":"vpc","provider":"aws","account_id":%d,"region":"us-east-1","name":"managed-vpc","cidr":"10.60.0.0/16","provider_resource_id":"vpc-managed"}`, account.ID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/objects", body, http.StatusCreated)
	var obj domain.NetworkObject
	if err := json.Unmarshal(rr.Body.Bytes(), &obj); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if obj.ID == 0 || obj.ObjectType != domain.NetworkObjectTypeVPC || obj.State != domain.NetworkObjectStateManaged {
		t.Fatalf("unexpected created object: %+v", obj)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodPatch, fmt.Sprintf("/api/v1/network/objects/%d", obj.ID), `{"state":"imported","name":"renamed-vpc"}`, http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &obj); err != nil {
		t.Fatalf("unmarshal updated object: %v", err)
	}
	if obj.Name != "renamed-vpc" || obj.State != domain.NetworkObjectStateImported {
		t.Fatalf("object update did not apply: %+v", obj)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/flat?object_type=vpc", "", http.StatusOK)
	var view domain.NetworkViewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	var sawManaged bool
	for _, item := range view.Items {
		if item.Kind == "network_object" && item.Name == "renamed-vpc" && item.CIDR == "10.60.0.0/16" {
			sawManaged = true
		}
	}
	if !sawManaged {
		t.Fatalf("managed network object missing from merged view: %+v", view.Items)
	}
}

func TestNetworkRelationshipsCreateFilterResolveAndAttachToMergedView(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	objBody := fmt.Sprintf(`{"object_type":"network","provider":"aws","account_id":%d,"name":"managed-net"}`, account.ID)
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/objects", objBody, http.StatusCreated)
	var obj domain.NetworkObject
	if err := json.Unmarshal(rr.Body.Bytes(), &obj); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}

	relBody := fmt.Sprintf(`{"id":"rel-test","type":"contains","source_kind":"network_object","source_id":"%d","target_kind":"pool","target_id":"42","confidence":0.5,"evidence":["manual"]}`, obj.ID)
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/relationships", relBody, http.StatusCreated)
	var rel domain.NetworkRelationship
	if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
		t.Fatalf("unmarshal relationship: %v", err)
	}
	if rel.ID != "rel-test" || rel.ResolutionState != "open" {
		t.Fatalf("unexpected relationship: %+v", rel)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/relationships?type=contains&source_kind=network_object", "", http.StatusOK)
	var rels domain.NetworkRelationshipListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &rels); err != nil {
		t.Fatalf("unmarshal relationships: %v", err)
	}
	if rels.Total != 1 {
		t.Fatalf("relationship filter returned %+v", rels)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/relationships/rel-test/resolve", `{"resolution_state":"resolved","reason":"accepted"}`, http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
		t.Fatalf("unmarshal resolved relationship: %v", err)
	}
	if rel.ResolutionState != "resolved" || rel.Reason != "accepted" {
		t.Fatalf("relationship resolution did not persist: %+v", rel)
	}

	slashIDBody := fmt.Sprintf(`{"id":"tenant/a","type":"contains","source_kind":"network_object","source_id":"%d","target_kind":"pool","target_id":"43","confidence":0.5}`, obj.ID)
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/relationships", slashIDBody, http.StatusCreated)
	if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
		t.Fatalf("unmarshal slash relationship: %v", err)
	}
	if rel.ID != "tenant/a" {
		t.Fatalf("caller relationship ID with slash was not preserved: %+v", rel)
	}
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/relationships/resolve", `{"id":"tenant/a","resolution_state":"ignored","reason":"body lookup"}`, http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
		t.Fatalf("unmarshal body-resolved relationship: %v", err)
	}
	if rel.ResolutionState != "ignored" || rel.Reason != "body lookup" {
		t.Fatalf("body relationship resolution did not persist: %+v", rel)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/flat?q=managed-net", "", http.StatusOK)
	var view domain.NetworkViewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("relationship not attached to merged node: %+v", view.Items)
	}
	var sawResolved, sawIgnored bool
	for _, rel := range view.Items[0].Relationships {
		if rel.ID == "rel-test" && rel.ResolutionState == "resolved" {
			sawResolved = true
		}
		if rel.ID == "tenant/a" && rel.ResolutionState == "ignored" {
			sawIgnored = true
		}
	}
	if !sawResolved || !sawIgnored {
		t.Fatalf("relationships not attached to merged node: %+v", view.Items[0].Relationships)
	}
}

func TestComputedNetworkRelationshipResolutionSurvivesRecompute(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	_, err = st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-vpc", CIDR: "10.61.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
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
		ResourceID:   "vpc-prod",
		CIDR:         "10.61.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=unlinked_exact_pool", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 || len(conflicts.Items[0].Relationships) == 0 {
		t.Fatalf("computed relationships missing from first conflict response: %+v", conflicts)
	}
	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/relationships?source_kind=discovered&source_id="+vpcID.String(), "", http.StatusOK)
	var rels domain.NetworkRelationshipListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &rels); err != nil {
		t.Fatalf("unmarshal relationships: %v", err)
	}
	if rels.Total == 0 {
		t.Fatalf("expected computed relationship")
	}
	relID := rels.Items[0].ID
	if strings.ContainsAny(relID, "/?#") {
		t.Fatalf("server-generated relationship ID is not URL-safe: %q", relID)
	}
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/network/relationships/"+relID+"/resolve", `{"resolution_state":"resolved","reason":"accepted"}`, http.StatusOK)
	doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=unlinked_exact_pool", "", http.StatusOK)

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/relationships?source_kind=discovered&source_id="+vpcID.String(), "", http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &rels); err != nil {
		t.Fatalf("unmarshal relationships after recompute: %v", err)
	}
	var found bool
	for _, rel := range rels.Items {
		if rel.ID == relID {
			found = true
			if rel.ResolutionState != "resolved" {
				t.Fatalf("computed relationship state was overwritten: %+v", rel)
			}
		}
	}
	if !found {
		t.Fatalf("computed relationship not found after recompute: %+v", rels.Items)
	}
}

func TestNetworkHierarchyPlacesVPCUnderPoolAndSubnetUnderVPC(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	_, err = st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-space", CIDR: "10.80.0.0/12", Type: domain.PoolTypeSupernet})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	now := time.Now().UTC()
	vpcID := uuid.New()
	subnetID := uuid.New()
	parent := "vpc-1"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: vpcID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-1", Name: "prod-vpc", CIDR: "10.80.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: subnetID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeSubnet, ResourceID: "subnet-1", Name: "prod-subnet", CIDR: "10.80.1.0/24", ParentResourceID: &parent, Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/hierarchy", "", http.StatusOK)
	var resp domain.NetworkViewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	if !hierarchyHasParentChild(resp.Items, "pool:", "discovered:"+vpcID.String()) {
		t.Fatalf("VPC was not nested under containing pool: %+v", resp.Items)
	}
	if !hierarchyHasParentChild(resp.Items, "discovered:"+vpcID.String(), "discovered:"+subnetID.String()) {
		t.Fatalf("subnet was not nested under VPC: %+v", resp.Items)
	}
}

func TestNetworkConflictsExposeMissingParentAndResolveRequest(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	parent := "vpc-missing"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "orphan-subnet",
		CIDR:             "10.90.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var resp domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].Type != "missing_parent" {
		t.Fatalf("unexpected conflicts: %+v", resp)
	}
	if strings.Join(resp.Items[0].AvailableDecisions, ",") != "skip,ignore,defer" {
		t.Fatalf("available decisions = %v, want skip/ignore/defer", resp.Items[0].AvailableDecisions)
	}

	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/resolve", resp.Items[0].ID), `{"decision":"link"}`, http.StatusBadRequest)

	body := `{"decision":"skip","reason":"parent intentionally absent"}`
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/resolve", resp.Items[0].ID), body, http.StatusOK)
	var resolved domain.NetworkConflict
	if err := json.Unmarshal(rr.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("unmarshal resolve: %v", err)
	}
	if resolved.Status != "resolved" || resolved.ResolutionState != "resolved" || resolved.ResolutionRequested != "skip" {
		t.Fatalf("unexpected resolve response: %+v", resolved)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal conflicts after resolve: %v", err)
	}
	if resp.Items[0].Status != "resolved" || resp.Items[0].ResolutionRequested != "skip" {
		t.Fatalf("resolution was not durable in computed conflict view: %+v", resp.Items[0])
	}
}

func TestNetworkConflictCreatePlaceholderParentAction(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	parent := "vpc-placeholder"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "orphan-subnet",
		CIDR:             "10.91.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected missing-parent conflict, got %+v", conflicts)
	}

	body := fmt.Sprintf(`{"discovered_id":"%s","name":"placeholder-vpc","reason":"parent not yet scanned"}`, subnetID)
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/create_placeholder_parent", conflicts.Items[0].ID), body, http.StatusOK)
	var action domain.NetworkConflictActionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &action); err != nil {
		t.Fatalf("unmarshal action: %v", err)
	}
	if action.Action != "create_placeholder_parent" || action.NetworkObject == nil || action.NetworkObject.State != domain.NetworkObjectStatePlaceholder {
		t.Fatalf("unexpected placeholder action: %+v", action)
	}
	if len(action.Relationships) != 2 {
		t.Fatalf("expected placeholder relationships, got %+v", action.Relationships)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/objects?state=placeholder", "", http.StatusOK)
	var objects domain.NetworkObjectListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &objects); err != nil {
		t.Fatalf("unmarshal objects: %v", err)
	}
	if objects.Total != 1 || objects.Items[0].ProviderResourceID != parent {
		t.Fatalf("placeholder object not durable/listable: %+v", objects)
	}
}

func TestNetworkSchemaPolicyChangesDuplicateDetection(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	now := time.Now().UTC()
	for _, res := range []domain.DiscoveredResource{
		{ID: uuid.New(), AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-east", CIDR: "10.92.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
		{ID: uuid.New(), AccountID: account.ID, Provider: "aws", Region: "us-west-2", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-west", CIDR: "10.92.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
	} {
		upsertDiscoveredForImportTest(t, ds, res)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=duplicate_cidr", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal default conflicts: %v", err)
	}
	if conflicts.Total != 0 {
		t.Fatalf("account-level policy should allow same-CIDR reuse inside one account, got %+v", conflicts)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=duplicate_cidr&schema_policy=global", "", http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal global conflicts: %v", err)
	}
	if conflicts.Total != 1 || !strings.Contains(strings.Join(conflicts.Items[0].Evidence, ","), "policy=global") {
		t.Fatalf("global policy should flag duplicate with evidence, got %+v", conflicts)
	}

	rr = doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=duplicate_cidr&schema_policy=manual", "", http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal manual conflicts: %v", err)
	}
	if conflicts.Total != 0 {
		t.Fatalf("manual policy should suppress duplicate conflicts, got %+v", conflicts)
	}
}

func TestNetworkConflictRoutesAppearInOpenAPISpec(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rr := httptest.NewRecorder()
	discSrv.srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`"/api/v1/network/conflicts":`,
		`"/api/v1/network/conflicts/{conflictId}/resolve":`,
		`"/api/v1/network/conflicts/{conflictId}/actions/link":`,
		`"/api/v1/network/conflicts/{conflictId}/actions/import":`,
		`"/api/v1/network/conflicts/{conflictId}/actions/create_placeholder_parent":`,
		`"/api/v1/network/objects":`,
		`"/api/v1/network/relationships":`,
		`"/api/v1/network/relationships/resolve":`,
		`$ref: '#/components/schemas/ResolveNetworkConflictRequest'`,
		`$ref: '#/components/schemas/NetworkConflictLinkActionRequest'`,
		`$ref: '#/components/schemas/NetworkConflictImportActionRequest'`,
		`$ref: '#/components/schemas/NetworkConflictPlaceholderParentActionRequest'`,
		`$ref: '#/components/schemas/NetworkConflictActionResponse'`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("generated OpenAPI spec missing %s", want)
		}
	}
}

func TestNetworkConflictLinkActionLinksExactPoolAndResolves(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-vpc", CIDR: "10.100.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	now := time.Now().UTC()
	vpcID := uuid.New()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:           vpcID,
		AccountID:    account.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-prod",
		Name:         "prod-vpc",
		CIDR:         "10.100.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=unlinked_exact_pool", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected one exact-pool conflict, got %+v", conflicts)
	}

	body := fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d,"reason":"exact match reviewed"}`, vpcID, pool.ID)
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), body, http.StatusOK)
	var action domain.NetworkConflictActionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &action); err != nil {
		t.Fatalf("unmarshal link action: %v", err)
	}
	if action.Action != "link" || !action.ResourceLinked || action.Conflict.ResolutionRequested != "link" {
		t.Fatalf("unexpected link action response: %+v", action)
	}
	linked, err := ds.GetDiscoveredResource(t.Context(), vpcID)
	if err != nil {
		t.Fatalf("load linked resource: %v", err)
	}
	if linked.PoolID == nil || *linked.PoolID != pool.ID {
		t.Fatalf("resource was not linked to pool: %+v", linked)
	}
}

func TestNetworkConflictLinkActionRejectsUnrelatedAndUnsafePayloads(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	otherAccount, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:222222222222", Name: "dev", Provider: "aws"})
	if err != nil {
		t.Fatalf("create other account: %v", err)
	}
	mismatchPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "mismatch", CIDR: "10.110.0.0/16", Type: domain.PoolTypeVPC, AccountID: &otherAccount.ID})
	if err != nil {
		t.Fatalf("create mismatch pool: %v", err)
	}
	unrelatedPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "unrelated", CIDR: "10.111.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
	if err != nil {
		t.Fatalf("create unrelated pool: %v", err)
	}

	now := time.Now().UTC()
	vpcID := uuid.New()
	otherID := uuid.New()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: vpcID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-prod", CIDR: "10.110.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: otherID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-other", CIDR: "10.112.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=unlinked_exact_pool", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected one exact-pool conflict, got %+v", conflicts)
	}

	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d}`, otherID, mismatchPool.ID), http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d}`, vpcID, unrelatedPool.ID), http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d}`, vpcID, mismatchPool.ID), http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d,"override":true}`, otherID, mismatchPool.ID), http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d,"override":true}`, vpcID, unrelatedPool.ID), http.StatusBadRequest)
}

func TestNetworkConflictImportActionImportsMissingParentWithOverride(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	parentPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-space", CIDR: "10.120.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
	if err != nil {
		t.Fatalf("create parent pool: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	parent := "vpc-missing"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "subnet-1",
		CIDR:             "10.120.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected missing-parent conflict, got %+v", conflicts)
	}

	noOverride := fmt.Sprintf(`{"resource_ids":["%s"],"pool_id":%d}`, subnetID, parentPool.ID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), noOverride, http.StatusBadRequest)

	body := fmt.Sprintf(`{"resource_ids":["%s"],"pool_id":%d,"override":true,"reason":"use containing pool"}`, subnetID, parentPool.ID)
	rr = doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), body, http.StatusOK)
	var action domain.NetworkConflictActionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &action); err != nil {
		t.Fatalf("unmarshal import action: %v", err)
	}
	if action.Action != "import" || action.Import == nil || action.Import.PoolsCreated != 1 || action.Import.ResourcesLinked != 1 {
		t.Fatalf("unexpected import action response: %+v", action)
	}
	linked, err := ds.GetDiscoveredResource(t.Context(), subnetID)
	if err != nil {
		t.Fatalf("load imported resource: %v", err)
	}
	if linked.PoolID == nil {
		t.Fatalf("resource was not linked after import: %+v", linked)
	}
}

func TestNetworkConflictImportActionRejectsCrossAccountParentPoolWithOverride(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	otherAccount, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:222222222222", Name: "dev", Provider: "aws"})
	if err != nil {
		t.Fatalf("create other account: %v", err)
	}
	otherParentPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "dev-space", CIDR: "10.125.0.0/16", Type: domain.PoolTypeVPC, AccountID: &otherAccount.ID})
	if err != nil {
		t.Fatalf("create other parent pool: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	parent := "vpc-missing"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "subnet-1",
		CIDR:             "10.125.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected missing-parent conflict, got %+v", conflicts)
	}

	body := fmt.Sprintf(`{"resource_ids":["%s"],"pool_id":%d,"override":true}`, subnetID, otherParentPool.ID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), body, http.StatusBadRequest)
	linked, err := ds.GetDiscoveredResource(t.Context(), subnetID)
	if err != nil {
		t.Fatalf("load subnet after rejected import: %v", err)
	}
	if linked.PoolID != nil {
		t.Fatalf("cross-account import should not link resource, got pool %d", *linked.PoolID)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools after rejected import: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("cross-account import should not create pools, got %d pools: %+v", len(pools), pools)
	}
}

func TestNetworkConflictImportActionRejectsImplicitCrossAccountParentPoolWithOverride(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	otherAccount, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:222222222222", Name: "dev", Provider: "aws"})
	if err != nil {
		t.Fatalf("create other account: %v", err)
	}
	otherParentPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "dev-space", CIDR: "10.126.0.0/16", Type: domain.PoolTypeVPC, AccountID: &otherAccount.ID})
	if err != nil {
		t.Fatalf("create other parent pool: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	parent := "vpc-missing"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{
		ID:               subnetID,
		AccountID:        account.ID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     domain.ResourceTypeSubnet,
		ResourceID:       "subnet-1",
		Name:             "subnet-1",
		CIDR:             "10.126.1.0/24",
		ParentResourceID: &parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected missing-parent conflict, got %+v", conflicts)
	}

	body := fmt.Sprintf(`{"resource_ids":["%s"],"override":true}`, subnetID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), body, http.StatusBadRequest)
	linked, err := ds.GetDiscoveredResource(t.Context(), subnetID)
	if err != nil {
		t.Fatalf("load subnet after rejected import: %v", err)
	}
	if linked.PoolID != nil {
		t.Fatalf("implicit cross-account import should not link resource, got pool %d", *linked.PoolID)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools after rejected import: %v", err)
	}
	if len(pools) != 1 || pools[0].ID != otherParentPool.ID {
		t.Fatalf("implicit cross-account import should not create pools, got %+v", pools)
	}
}

func TestNetworkConflictImportActionRejectsUnrelatedResource(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	otherID := uuid.New()
	parent := "vpc-missing"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: subnetID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeSubnet, ResourceID: "subnet-1", CIDR: "10.130.1.0/24", ParentResourceID: &parent, Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: otherID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-other", CIDR: "10.131.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected missing-parent conflict, got %+v", conflicts)
	}
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), fmt.Sprintf(`{"resource_ids":["%s"]}`, otherID), http.StatusBadRequest)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), fmt.Sprintf(`{"resource_ids":["%s"],"override":true}`, otherID), http.StatusBadRequest)
}

func TestNetworkConflictImportActionRejectsPartialApplyAndRollsBack(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	parentPool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-space", CIDR: "10.135.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
	if err != nil {
		t.Fatalf("create parent pool: %v", err)
	}

	now := time.Now().UTC()
	subnetID := uuid.New()
	eipID := uuid.New()
	parent := "vpc-missing"
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: subnetID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeSubnet, ResourceID: "subnet-1", CIDR: "10.135.1.0/24", ParentResourceID: &parent, Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: eipID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeElasticIP, ResourceID: "eip-1", Name: "eip-1", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})

	rr := doJSON(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=missing_parent", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected missing-parent conflict, got %+v", conflicts)
	}

	body := fmt.Sprintf(`{"resource_ids":["%s","%s"],"pool_id":%d,"override":true}`, subnetID, eipID, parentPool.ID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/import", conflicts.Items[0].ID), body, http.StatusBadRequest)

	linked, err := ds.GetDiscoveredResource(t.Context(), subnetID)
	if err != nil {
		t.Fatalf("load subnet after failed partial import: %v", err)
	}
	if linked.PoolID != nil {
		t.Fatalf("partial import should rollback subnet link, got pool %d", *linked.PoolID)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools after failed partial import: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("partial import should rollback created pools, got %d pools: %+v", len(pools), pools)
	}
}

func TestNetworkConflictLinkActionRollsBackWhenResolutionPersistenceFails(t *testing.T) {
	srv, st := setupTestServer()
	ds := storage.NewMemoryDiscoveryStore(st)
	driftStore := &failResolvedDriftStore{base: storage.NewMemoryDriftStore(st)}
	networkSrv := NewNetworkServer(srv, st, ds, driftStore)
	networkSrv.RegisterNetworkRoutes()

	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-vpc", CIDR: "10.140.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	now := time.Now().UTC()
	vpcID := uuid.New()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: vpcID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-prod", CIDR: "10.140.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})

	rr := doJSON(t, srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=unlinked_exact_pool", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected one exact-pool conflict, got %+v", conflicts)
	}

	body := fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d}`, vpcID, pool.ID)
	doJSON(t, srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), body, http.StatusInternalServerError)
	res, err := ds.GetDiscoveredResource(t.Context(), vpcID)
	if err != nil {
		t.Fatalf("load resource after rollback: %v", err)
	}
	if res.PoolID != nil {
		t.Fatalf("expected failed resolution persistence to rollback link, got pool %d", *res.PoolID)
	}
}

func TestNetworkConflictActionUpdatesExistingDriftDetails(t *testing.T) {
	srv, st := setupTestServer()
	ds := storage.NewMemoryDiscoveryStore(st)
	driftStore := storage.NewMemoryDriftStore(st)
	networkSrv := NewNetworkServer(srv, st, ds, driftStore)
	networkSrv.RegisterNetworkRoutes()

	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:123456789012", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod-vpc", CIDR: "10.150.0.0/16", Type: domain.PoolTypeVPC, AccountID: &account.ID})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	now := time.Now().UTC()
	vpcID := uuid.New()
	upsertDiscoveredForImportTest(t, ds, domain.DiscoveredResource{ID: vpcID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-prod", CIDR: "10.150.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now})

	rr := doJSON(t, srv.mux, http.MethodGet, "/api/v1/network/conflicts?conflict_type=unlinked_exact_pool", "", http.StatusOK)
	var conflicts domain.NetworkConflictListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &conflicts); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if conflicts.Total != 1 {
		t.Fatalf("expected one exact-pool conflict, got %+v", conflicts)
	}
	existing := domain.DriftItem{
		ID:          conflicts.Items[0].ID,
		AccountID:   account.ID,
		Type:        domain.DriftTypeAccountDrift,
		Severity:    domain.DriftSeverityWarning,
		Status:      domain.DriftStatusOpen,
		Title:       conflicts.Items[0].Title,
		Description: conflicts.Items[0].Description,
		Details:     map[string]string{"existing": "true"},
		DetectedAt:  now,
		UpdatedAt:   now,
	}
	if err := driftStore.CreateDriftItem(t.Context(), existing); err != nil {
		t.Fatalf("create existing drift: %v", err)
	}

	body := fmt.Sprintf(`{"discovered_id":"%s","pool_id":%d}`, vpcID, pool.ID)
	doJSON(t, srv.mux, http.MethodPost, fmt.Sprintf("/api/v1/network/conflicts/%s/actions/link", conflicts.Items[0].ID), body, http.StatusOK)
	item, err := driftStore.GetDriftItem(t.Context(), conflicts.Items[0].ID)
	if err != nil {
		t.Fatalf("get drift item: %v", err)
	}
	if item.Details["existing"] != "true" || item.Details["network_conflict_action"] != "link" || item.Details["pool_id"] != fmt.Sprintf("%d", pool.ID) {
		t.Fatalf("existing drift details were not merged with action details: %+v", item.Details)
	}
}

type failResolvedDriftStore struct {
	base *storage.MemoryDriftStore
}

func (s *failResolvedDriftStore) CreateDriftItem(ctx context.Context, item domain.DriftItem) error {
	return s.base.CreateDriftItem(ctx, item)
}

func (s *failResolvedDriftStore) GetDriftItem(ctx context.Context, id string) (*domain.DriftItem, error) {
	return s.base.GetDriftItem(ctx, id)
}

func (s *failResolvedDriftStore) ListDriftItems(ctx context.Context, filters domain.DriftFilters) ([]domain.DriftItem, int, error) {
	return s.base.ListDriftItems(ctx, filters)
}

func (s *failResolvedDriftStore) UpdateDriftStatus(ctx context.Context, id string, status domain.DriftStatus, ignoreReason string) error {
	if status == domain.DriftStatusResolved {
		return errors.New("forced final resolution failure")
	}
	return s.base.UpdateDriftStatus(ctx, id, status, ignoreReason)
}

func (s *failResolvedDriftStore) UpdateDriftDetails(ctx context.Context, id string, details map[string]string) error {
	return s.base.UpdateDriftDetails(ctx, id, details)
}

func (s *failResolvedDriftStore) DeleteOpenForAccount(ctx context.Context, accountID int64) error {
	return s.base.DeleteOpenForAccount(ctx, accountID)
}

func hierarchyHasParentChild(nodes []domain.NetworkNode, parentPrefix string, childID string) bool {
	for _, node := range nodes {
		parentMatches := strings.HasPrefix(node.ID, parentPrefix)
		for _, child := range node.Children {
			if parentMatches && child.ID == childID {
				return true
			}
			if hierarchyHasParentChild([]domain.NetworkNode{child}, parentPrefix, childID) {
				return true
			}
		}
	}
	return false
}
