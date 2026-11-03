package api

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

func setupTypedRecommendationServer() (*stdhttp.ServeMux, *storage.MemoryStore, storage.RecommendationStore) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	srv := NewServerWithSlog(mux, st, nil)
	srv.registerUnprotectedTestRoutes()

	recStore := storage.NewMemoryRecommendationStore(st)
	recSvc := planning.NewRecommendationService(planning.NewAnalysisService(st), recStore, st, storage.NewMemoryDiscoveryStore(st), srv)
	NewRecommendationServer(srv, recSvc, recStore).RegisterRecommendationRoutes()
	return mux, st, recStore
}

func TestRecommendationHandler_ConsolidationListsAndApplies(t *testing.T) {
	mux, st := setupRecommendationServer()
	ctx := context.Background()
	parent, _ := st.CreatePool(ctx, domain.CreatePool{Name: "Corp", CIDR: "10.0.0.0/16", Description: "corp"})
	a, _ := st.CreatePool(ctx, domain.CreatePool{Name: "a", CIDR: "10.0.1.0/25", ParentID: &parent.ID, Description: "a"})
	b, _ := st.CreatePool(ctx, domain.CreatePool{Name: "b", CIDR: "10.0.1.128/25", ParentID: &parent.ID, Description: "b"})

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(`{"pool_ids":[`+int64Str(parent.ID)+`]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("generate: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/recommendations?type=consolidation", nil))
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	var list domain.RecommendationsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].Type != domain.RecommendationTypeConsolidation || list.Items[0].SuggestedCIDR != "10.0.1.0/24" {
		t.Fatalf("list by type=consolidation returned %+v, want one merge into 10.0.1.0/24", list)
	}
	rec := list.Items[0]

	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/"+rec.ID+"/apply", strings.NewReader(`{"name":"Merged"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	var applied domain.Recommendation
	if err := json.NewDecoder(rr.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != domain.RecommendationStatusApplied || applied.AppliedPoolID == nil {
		t.Fatalf("applied %+v", applied)
	}
	merged, found, _ := st.GetPool(ctx, *applied.AppliedPoolID)
	if !found || merged.CIDR != "10.0.1.0/24" || merged.Name != "Merged" {
		t.Errorf("merged pool %+v", merged)
	}
	for _, src := range []int64{a.ID, b.ID} {
		if _, found, _ := st.GetPool(ctx, src); found {
			t.Errorf("source pool %d still exists", src)
		}
	}
}

func TestRecommendationHandler_ReclaimApplyConflictsWhenPoolChanged(t *testing.T) {
	mux, st, recStore := setupTypedRecommendationServer()
	ctx := context.Background()
	pool, _ := st.CreatePool(ctx, domain.CreatePool{Name: "Idle", CIDR: "10.0.0.0/16", Description: "idle"})
	rec := domain.Recommendation{
		ID: "reclaim-1", PoolID: pool.ID, Type: domain.RecommendationTypeReclaim,
		Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh,
		Title: "Reclaim 10.0.0.0/16", SuggestedCIDR: pool.CIDR, Score: 90,
	}
	if err := recStore.CreateRecommendation(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: "10.0.0.0/24", ParentID: &pool.ID}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/reclaim-1/apply", strings.NewReader(`{}`)))
	if rr.Code != stdhttp.StatusConflict {
		t.Fatalf("apply reclaim on a pool that grew a child: %d %s, want 409", rr.Code, rr.Body.String())
	}
	if _, found, _ := st.GetPool(ctx, pool.ID); !found {
		t.Error("pool was deleted although the apply was refused")
	}
}
