package http

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

func setupAnalysisServer() (*stdhttp.ServeMux, *storage.MemoryStore) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, st, logger, nil, nil)
	srv.RegisterRoutes()
	analysisSvc := planning.NewAnalysisService(st)
	analysisSrv := NewAnalysisServer(srv, analysisSvc)
	analysisSrv.RegisterAnalysisRoutes()
	return mux, st
}

func TestAnalysisHandler_FullReport(t *testing.T) {
	mux, st := setupAnalysisServer()

	parent, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet,
	})
	parentID := parent.ID
	st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Sub", CIDR: "10.0.0.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})

	body := `{"pool_ids":[` + int64Str(parent.ID) + `],"include_children":true}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var report planning.NetworkAnalysisReport
	if err := json.NewDecoder(rr.Body).Decode(&report); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if report.Summary.TotalPools == 0 {
		t.Error("expected total_pools > 0")
	}
	if len(report.GapAnalyses) == 0 {
		t.Error("expected gap analyses")
	}
}

func TestAnalysisHandler_MethodNotAllowed(t *testing.T) {
	mux, _ := setupAnalysisServer()

	endpoints := []string{
		"/api/v1/analysis",
		"/api/v1/analysis/gaps",
		"/api/v1/analysis/fragmentation",
		"/api/v1/analysis/compliance",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(stdhttp.MethodGet, ep, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != stdhttp.StatusMethodNotAllowed {
			t.Errorf("%s GET: expected 405, got %d", ep, rr.Code)
		}
	}
}

func TestAnalysisHandler_Gaps(t *testing.T) {
	mux, st := setupAnalysisServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Pool", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSubnet,
	})

	body := `{"pool_id":` + int64Str(pool.ID) + `}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/gaps", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var gap planning.GapAnalysis
	if err := json.NewDecoder(rr.Body).Decode(&gap); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if gap.TotalAddresses != 256 {
		t.Errorf("total_addresses = %d, want 256", gap.TotalAddresses)
	}
}

func TestAnalysisHandler_Gaps_MissingPoolID(t *testing.T) {
	mux, _ := setupAnalysisServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/gaps", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAnalysisHandler_Gaps_NotFound(t *testing.T) {
	mux, _ := setupAnalysisServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/gaps", strings.NewReader(`{"pool_id":999}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAnalysisHandler_Fragmentation(t *testing.T) {
	mux, st := setupAnalysisServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Pool", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet,
	})

	body := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/fragmentation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAnalysisHandler_Fragmentation_MissingPoolIDs(t *testing.T) {
	mux, _ := setupAnalysisServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/fragmentation", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAnalysisHandler_Compliance(t *testing.T) {
	mux, st := setupAnalysisServer()

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Pool", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSubnet,
	})

	body := `{"pool_ids":[` + int64Str(pool.ID) + `]}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/compliance", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var report planning.ComplianceReport
	if err := json.NewDecoder(rr.Body).Decode(&report); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if report.TotalChecks == 0 {
		t.Error("expected checks to run")
	}
}

func TestAnalysisHandler_Compliance_AllPools(t *testing.T) {
	mux, st := setupAnalysisServer()

	st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Pool A", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSubnet,
	})
	st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Pool B", CIDR: "172.16.0.0/16", Type: domain.PoolTypeSubnet,
	})

	// Empty pool_ids means all pools.
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/analysis/compliance", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAnalysisHandler_InvalidBody(t *testing.T) {
	mux, _ := setupAnalysisServer()

	endpoints := []string{
		"/api/v1/analysis",
		"/api/v1/analysis/gaps",
		"/api/v1/analysis/fragmentation",
		"/api/v1/analysis/compliance",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(stdhttp.MethodPost, ep, strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != stdhttp.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", ep, rr.Code)
		}
	}
}

func int64Str(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
