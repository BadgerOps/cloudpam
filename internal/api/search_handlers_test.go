package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func setupSearchServer(t *testing.T) (*Server, *storage.MemoryStore) {
	t.Helper()
	store := storage.NewMemoryStore()
	mux := http.NewServeMux()
	srv := NewServer(mux, store, nil, nil, nil)
	srv.registerUnprotectedTestRoutes()
	return srv, store
}

func seedSearchData(t *testing.T, store *storage.MemoryStore) {
	t.Helper()
	ctx := t.Context()
	// Create pools
	if _, err := store.CreatePool(ctx, domain.CreatePool{Name: "Corporate", CIDR: "10.0.0.0/8", Type: domain.PoolTypeSupernet}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool(ctx, domain.CreatePool{Name: "US East", CIDR: "10.1.0.0/16", Type: domain.PoolTypeRegion, ParentID: int64Ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool(ctx, domain.CreatePool{Name: "Production VPC", CIDR: "10.1.1.0/24", Type: domain.PoolTypeVPC, ParentID: int64Ptr(2)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool(ctx, domain.CreatePool{Name: "Staging", CIDR: "172.16.0.0/16", Type: domain.PoolTypeEnvironment}); err != nil {
		t.Fatal(err)
	}
	// Create accounts
	if _, err := store.CreateAccount(ctx, domain.CreateAccount{Key: "aws:prod", Name: "AWS Production", Provider: "aws"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAccount(ctx, domain.CreateAccount{Key: "gcp:dev", Name: "GCP Development", Provider: "gcp"}); err != nil {
		t.Fatal(err)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestSearchByName(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=production", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.Total < 2 {
		t.Errorf("expected at least 2 results (pool + account), got %d", resp.Total)
	}

	// Should find both "Production VPC" pool and "AWS Production" account
	types := map[string]bool{}
	for _, item := range resp.Items {
		types[item.Type] = true
	}
	if !types["pool"] {
		t.Error("expected pool result for 'production' search")
	}
	if !types["account"] {
		t.Error("expected account result for 'production' search")
	}
}

func TestSearchByCIDRContains(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	// Search for pools containing 10.1.1.5/32
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?cidr_contains=10.1.1.5", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// 10.1.1.5 is contained by: 10.0.0.0/8, 10.1.0.0/16, 10.1.1.0/24
	if resp.Total != 3 {
		t.Errorf("expected 3 containing pools, got %d", resp.Total)
		for _, item := range resp.Items {
			t.Logf("  found: %s %s", item.Name, item.CIDR)
		}
	}

	// Should not include 172.16.0.0/16
	for _, item := range resp.Items {
		if item.CIDR == "172.16.0.0/16" {
			t.Error("172.16.0.0/16 should not contain 10.1.1.5")
		}
	}
}

func TestSearchByCIDRWithin(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	// Search for pools within 10.0.0.0/8
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?cidr_within=10.0.0.0/8", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// 10.0.0.0/8 (exact), 10.1.0.0/16, 10.1.1.0/24 are within 10.0.0.0/8
	if resp.Total != 3 {
		t.Errorf("expected 3 pools within 10.0.0.0/8, got %d", resp.Total)
		for _, item := range resp.Items {
			t.Logf("  found: %s %s", item.Name, item.CIDR)
		}
	}

	// Should not include 172.16.0.0/16
	for _, item := range resp.Items {
		if item.CIDR == "172.16.0.0/16" {
			t.Error("172.16.0.0/16 should not be within 10.0.0.0/8")
		}
	}
}

func TestSearchTypeFilter(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	// Search only pools
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=production&type=pool", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp domain.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	for _, item := range resp.Items {
		if item.Type != "pool" {
			t.Errorf("expected only pool results, got %s", item.Type)
		}
	}
}

func TestSearchInvalidCIDR(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?cidr_contains=notacidr", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid CIDR, got %d", w.Code)
	}
}

func TestSearchNoQuery(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no query params, got %d", w.Code)
	}
}

func TestSearchPagination(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	// Request page 1 with page_size=2
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=&cidr_within=0.0.0.0/0&page=1&page_size=2", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.PageSize != 2 {
		t.Errorf("expected page_size=2, got %d", resp.PageSize)
	}
	if len(resp.Items) > 2 {
		t.Errorf("expected at most 2 items, got %d", len(resp.Items))
	}
	if resp.Total < 4 {
		t.Errorf("expected total >= 4 (all pools), got %d", resp.Total)
	}
}

func TestSearchMethodNotAllowed(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/search?q=test", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestSearchCombinedQueryAndCIDR(t *testing.T) {
	srv, store := setupSearchServer(t)
	seedSearchData(t, store)

	// Search for "east" within 10.0.0.0/8
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=east&cidr_within=10.0.0.0/8", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp domain.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// Only "US East" matches both text query and CIDR filter
	if resp.Total != 1 {
		t.Errorf("expected 1 result for 'east' within 10.0.0.0/8, got %d", resp.Total)
	}
	if resp.Total > 0 && resp.Items[0].Name != "US East" {
		t.Errorf("expected 'US East', got %q", resp.Items[0].Name)
	}
}
