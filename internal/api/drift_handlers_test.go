package api

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func setupDriftTestServer() (*stdhttp.ServeMux, *storage.MemoryStore, *storage.MemoryDiscoveryStore, *storage.MemoryDriftStore) {
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)

	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, ms, logger, nil, nil)

	detector := discovery.NewDriftDetector(ms, ds, driftStore)
	driftSrv := NewDriftServer(srv, detector, driftStore)

	// Register without RBAC for tests.
	mux.HandleFunc("/api/v1/drift/detect", driftSrv.handleDetect)
	mux.HandleFunc("/api/v1/drift", driftSrv.handleList)
	mux.HandleFunc("/api/v1/drift/", driftSrv.handleByID)

	return mux, ms, ds, driftStore
}

func doDriftJSON(t *testing.T, mux *stdhttp.ServeMux, method, path, body string, code int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != code {
		t.Fatalf("%s %s: expected code %d, got %d: %s", method, path, code, rr.Code, rr.Body.String())
	}
	return rr
}

func TestDriftHandlers_DetectAndList(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()

	// Create account and unlinked resource.
	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:test1", Name: "Test1", Provider: "aws"})
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           uuid.New(),
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-001",
		Name:         "test-vpc",
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})

	// Detect.
	rr := doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/detect", `{}`, stdhttp.StatusOK)
	var detectResp domain.RunDriftDetectionResponse
	if err := json.NewDecoder(rr.Body).Decode(&detectResp); err != nil {
		t.Fatal(err)
	}
	if detectResp.Total != 1 {
		t.Fatalf("expected 1 drift item, got %d", detectResp.Total)
	}

	// List.
	rr = doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift?status=open", "", stdhttp.StatusOK)
	var listResp domain.DriftListResponse
	if err := json.NewDecoder(rr.Body).Decode(&listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Total != 1 {
		t.Fatalf("expected 1 item in list, got %d", listResp.Total)
	}
}

func TestDriftHandlers_ResolveAndIgnore(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()

	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:test2", Name: "Test2", Provider: "aws"})
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           uuid.New(),
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-west-2",
		ResourceType: domain.ResourceTypeSubnet,
		ResourceID:   "subnet-001",
		Name:         "web-subnet",
		CIDR:         "10.1.0.0/24",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           uuid.New(),
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-west-2",
		ResourceType: domain.ResourceTypeSubnet,
		ResourceID:   "subnet-002",
		Name:         "db-subnet",
		CIDR:         "10.1.1.0/24",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})

	// Detect to create items.
	doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/detect", `{}`, stdhttp.StatusOK)

	// List to get IDs.
	rr := doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift?status=open", "", stdhttp.StatusOK)
	var listResp domain.DriftListResponse
	_ = json.NewDecoder(rr.Body).Decode(&listResp)
	if len(listResp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(listResp.Items))
	}

	id1 := listResp.Items[0].ID
	id2 := listResp.Items[1].ID

	// Resolve first.
	rr = doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/"+id1+"/resolve", `{}`, stdhttp.StatusOK)
	var resolved domain.DriftItem
	_ = json.NewDecoder(rr.Body).Decode(&resolved)
	if resolved.Status != domain.DriftStatusResolved {
		t.Fatalf("expected resolved, got %s", resolved.Status)
	}

	// Ignore second.
	rr = doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/"+id2+"/ignore", `{"reason":"expected"}`, stdhttp.StatusOK)
	var ignored domain.DriftItem
	_ = json.NewDecoder(rr.Body).Decode(&ignored)
	if ignored.Status != domain.DriftStatusIgnored {
		t.Fatalf("expected ignored, got %s", ignored.Status)
	}
	if ignored.IgnoreReason != "expected" {
		t.Fatalf("expected ignore reason 'expected', got %q", ignored.IgnoreReason)
	}

	// List open should be empty.
	rr = doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift?status=open", "", stdhttp.StatusOK)
	_ = json.NewDecoder(rr.Body).Decode(&listResp)
	if listResp.Total != 0 {
		t.Fatalf("expected 0 open items, got %d", listResp.Total)
	}
}

func TestDriftHandlers_GetByID(t *testing.T) {
	mux, ms, ds, _ := setupDriftTestServer()
	ctx := context.Background()

	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:test3", Name: "Test3", Provider: "aws"})
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           uuid.New(),
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "eu-west-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-get",
		Name:         "get-vpc",
		CIDR:         "172.16.0.0/12",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})

	doDriftJSON(t, mux, stdhttp.MethodPost, "/api/v1/drift/detect", `{}`, stdhttp.StatusOK)

	rr := doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift?status=open", "", stdhttp.StatusOK)
	var listResp domain.DriftListResponse
	_ = json.NewDecoder(rr.Body).Decode(&listResp)
	id := listResp.Items[0].ID

	// GET single item.
	rr = doDriftJSON(t, mux, stdhttp.MethodGet, "/api/v1/drift/"+id, "", stdhttp.StatusOK)
	var item domain.DriftItem
	_ = json.NewDecoder(rr.Body).Decode(&item)
	if item.ID != id {
		t.Fatalf("expected ID %s, got %s", id, item.ID)
	}
}
