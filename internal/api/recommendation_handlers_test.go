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

func setupRecommendationServer() (*stdhttp.ServeMux, *storage.MemoryStore) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	srv := NewServerWithSlog(mux, st, nil)
	srv.registerUnprotectedTestRoutes()

	analysisSvc := planning.NewAnalysisService(st)
	recStore := storage.NewMemoryRecommendationStore(st)
	recSvc := planning.NewRecommendationService(analysisSvc, recStore, st)
	recSrv := NewRecommendationServer(srv, recSvc, recStore)
	recSrv.RegisterRecommendationRoutes()

	return mux, st
}

func TestRecommendationHandler_Generate(t *testing.T) {
	mux, st := setupRecommendationServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	body := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp domain.GenerateRecommendationsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expected at least one recommendation")
	}
}

func TestRecommendationHandler_Generate_MissingPoolIDs(t *testing.T) {
	mux, _ := setupRecommendationServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRecommendationHandler_Generate_MethodNotAllowed(t *testing.T) {
	mux, _ := setupRecommendationServer()

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/recommendations/generate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestRecommendationHandler_List(t *testing.T) {
	mux, st := setupRecommendationServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	// Generate first.
	genBody := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	genReq := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genRR := httptest.NewRecorder()
	mux.ServeHTTP(genRR, genReq)

	// List.
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/recommendations", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp domain.RecommendationsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expected at least one recommendation in list")
	}
}

func TestRecommendationHandler_ListWithFilters(t *testing.T) {
	mux, st := setupRecommendationServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	genBody := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	genReq := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genRR := httptest.NewRecorder()
	mux.ServeHTTP(genRR, genReq)

	// Filter by type.
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/recommendations?type=allocation&status=pending", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp domain.RecommendationsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	for _, r := range resp.Items {
		if r.Type != domain.RecommendationTypeAllocation {
			t.Errorf("expected allocation type, got %s", r.Type)
		}
	}
}

func TestRecommendationHandler_Get(t *testing.T) {
	mux, st := setupRecommendationServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	genBody := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	genReq := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genRR := httptest.NewRecorder()
	mux.ServeHTTP(genRR, genReq)

	var genResp domain.GenerateRecommendationsResponse
	if err := json.NewDecoder(genRR.Body).Decode(&genResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(genResp.Items) == 0 {
		t.Fatal("no recommendations generated")
	}

	id := genResp.Items[0].ID
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/recommendations/"+id, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRecommendationHandler_GetNotFound(t *testing.T) {
	mux, _ := setupRecommendationServer()

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/recommendations/nonexistent-id", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestRecommendationHandler_Apply(t *testing.T) {
	mux, st := setupRecommendationServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	genBody := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	genReq := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genRR := httptest.NewRecorder()
	mux.ServeHTTP(genRR, genReq)

	var genResp domain.GenerateRecommendationsResponse
	if err := json.NewDecoder(genRR.Body).Decode(&genResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Find an allocation rec.
	var allocID string
	for _, r := range genResp.Items {
		if r.Type == domain.RecommendationTypeAllocation {
			allocID = r.ID
			break
		}
	}
	if allocID == "" {
		t.Fatal("no allocation recommendation found")
	}

	applyBody := `{"name":"New Subnet"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/"+allocID+"/apply", strings.NewReader(applyBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var rec domain.Recommendation
	if err := json.NewDecoder(rr.Body).Decode(&rec); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if rec.Status != domain.RecommendationStatusApplied {
		t.Errorf("expected applied, got %s", rec.Status)
	}
}

func TestRecommendationHandler_Dismiss(t *testing.T) {
	mux, st := setupRecommendationServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	genBody := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	genReq := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genRR := httptest.NewRecorder()
	mux.ServeHTTP(genRR, genReq)

	var genResp domain.GenerateRecommendationsResponse
	if err := json.NewDecoder(genRR.Body).Decode(&genResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(genResp.Items) == 0 {
		t.Fatal("no recommendations generated")
	}

	id := genResp.Items[0].ID
	dismissBody := `{"reason":"not needed"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/"+id+"/dismiss", strings.NewReader(dismissBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var rec domain.Recommendation
	if err := json.NewDecoder(rr.Body).Decode(&rec); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if rec.Status != domain.RecommendationStatusDismissed {
		t.Errorf("expected dismissed, got %s", rec.Status)
	}
}

func TestRecommendationHandler_InvalidBody(t *testing.T) {
	mux, _ := setupRecommendationServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/recommendations/generate", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
