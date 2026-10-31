package api

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

// Every paged list used to compute (page-1)*pageSize, which wraps negative for
// a page near MaxInt and either panicked on a slice bound or fetched page 1.
// A page past the end is an empty page that echoes the requested page.

var hugePages = []int{9223372036854775807, 9223372036854775807/200 + 2}

type pagedBody struct {
	Items    []json.RawMessage `json:"items"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

func getPaged(t *testing.T, mux *stdhttp.ServeMux, target string) pagedBody {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodGet, target, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != stdhttp.StatusOK {
		t.Fatalf("GET %s: status %d, want 200: %s", target, w.Code, w.Body.String())
	}
	var body pagedBody
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("GET %s: decode: %v", target, err)
	}
	return body
}

func assertEmptyPage(t *testing.T, target string, body pagedBody, wantTotal, wantPage, wantPageSize int) {
	t.Helper()
	if body.Total != wantTotal {
		t.Errorf("GET %s: total = %d, want %d", target, body.Total, wantTotal)
	}
	if len(body.Items) != 0 {
		t.Errorf("GET %s: len(items) = %d, want 0", target, len(body.Items))
	}
	if body.Page != wantPage || body.PageSize != wantPageSize {
		t.Errorf("GET %s: page/page_size = %d/%d, want %d/%d", target, body.Page, body.PageSize, wantPage, wantPageSize)
	}
}

func TestBlocksListHugePageReturnsEmptyPage(t *testing.T) {
	srv, st := setupTestServer()
	ctx := context.Background()
	parent, err := st.CreatePool(ctx, domain.CreatePool{Name: "parent", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	for _, cidr := range []string{"10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"} {
		if _, err := st.CreatePool(ctx, domain.CreatePool{Name: cidr, CIDR: cidr, ParentID: &parent.ID}); err != nil {
			t.Fatal(err)
		}
	}
	// The list counts every assigned block, so take the total from page 1.
	wantTotal := getPaged(t, srv.mux, "/api/v1/blocks?page_size=2&page=1").Total
	if wantTotal < 3 {
		t.Fatalf("page 1 total = %d, want at least the 3 children", wantTotal)
	}
	for _, page := range hugePages {
		target := "/api/v1/blocks?page_size=2&page=" + strconv.Itoa(page)
		assertEmptyPage(t, target, getPaged(t, srv.mux, target), wantTotal, page, 2)
	}
}

func TestPoolBlocksHugePageReturnsEmptyPage(t *testing.T) {
	srv, st := setupTestServer()
	pool, err := st.CreatePool(context.Background(), domain.CreatePool{Name: "parent", CIDR: "192.168.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/pools/" + strconv.FormatInt(pool.ID, 10) + "/blocks?new_prefix_len=20&page_size=4&page="
	for _, page := range hugePages {
		target := base + strconv.Itoa(page)
		assertEmptyPage(t, target, getPaged(t, srv.mux, target), 16, page, 4)
	}
	// A real page still works and is not confused with page 1.
	body := getPaged(t, srv.mux, base+"2")
	if body.Total != 16 || len(body.Items) != 4 || body.Page != 2 {
		t.Fatalf("page 2: total=%d len=%d page=%d, want 16/4/2", body.Total, len(body.Items), body.Page)
	}
	var first struct {
		CIDR string `json:"cidr"`
	}
	if err := json.Unmarshal(body.Items[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.CIDR != "192.168.64.0/20" {
		t.Errorf("page 2 first block = %s, want 192.168.64.0/20", first.CIDR)
	}
}

func TestDiscoveryResourcesHugePageReturnsEmptyPage(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	ctx := context.Background()
	account, err := st.CreateAccount(ctx, domain.CreateAccount{Key: "aws:111111111111", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []string{"vpc-a", "vpc-b", "vpc-c"} {
		if err := ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: id, CIDR: "10.0.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	base := "/api/v1/discovery/resources?account_id=" + strconv.FormatInt(account.ID, 10)
	for _, page := range hugePages {
		target := base + "&page_size=2&page=" + strconv.Itoa(page)
		assertEmptyPage(t, target, getPaged(t, discSrv.srv.mux, target), 3, page, 2)
	}
	// page_size is capped like search and the effective size is echoed.
	target := base + "&page_size=5000"
	body := getPaged(t, discSrv.srv.mux, target)
	if body.PageSize != storage.MaxPageSize || len(body.Items) != 3 || body.Page != 1 {
		t.Errorf("GET %s: page/page_size/len = %d/%d/%d, want 1/%d/3", target, body.Page, body.PageSize, len(body.Items), storage.MaxPageSize)
	}
}

func TestDriftListHugePageReturnsEmptyPage(t *testing.T) {
	mux, _, _, driftStore := setupDriftTestServer()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"d-a", "d-b", "d-c"} {
		if err := driftStore.CreateDriftItem(ctx, domain.DriftItem{ID: id, AccountID: 1, Type: domain.DriftTypeUnmanaged, Severity: domain.DriftSeverityWarning, Status: domain.DriftStatusOpen, Title: id, DetectedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range hugePages {
		target := "/api/v1/drift?page_size=2&page=" + strconv.Itoa(page)
		assertEmptyPage(t, target, getPaged(t, mux, target), 3, page, 2)
	}
	target := "/api/v1/drift?page_size=5000"
	if body := getPaged(t, mux, target); body.PageSize != storage.MaxPageSize {
		t.Errorf("GET %s: page_size = %d, want %d", target, body.PageSize, storage.MaxPageSize)
	}
}

func TestRecommendationsListHugePageReturnsEmptyPage(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	srv := NewServerWithSlog(mux, st, nil)
	recStore := storage.NewMemoryRecommendationStore(st)
	recSvc := planning.NewRecommendationService(planning.NewAnalysisService(st), recStore, st, srv)
	NewRecommendationServer(srv, recSvc, recStore).RegisterRecommendationRoutes()

	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"r-a", "r-b", "r-c"} {
		if err := recStore.CreateRecommendation(ctx, domain.Recommendation{ID: id, PoolID: 1, Type: domain.RecommendationTypeAllocation, Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh, Title: id, SuggestedCIDR: "10.0.5.0/24", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range hugePages {
		target := "/api/v1/recommendations?page_size=2&page=" + strconv.Itoa(page)
		assertEmptyPage(t, target, getPaged(t, mux, target), 3, page, 2)
	}
	target := "/api/v1/recommendations?page_size=5000"
	if body := getPaged(t, mux, target); body.PageSize != storage.MaxPageSize {
		t.Errorf("GET %s: page_size = %d, want %d", target, body.PageSize, storage.MaxPageSize)
	}
}
