package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
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
