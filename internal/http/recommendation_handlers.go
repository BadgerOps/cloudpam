package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

// RecommendationServer handles recommendation API endpoints.
type RecommendationServer struct {
	srv      *Server
	recSvc   *planning.RecommendationService
	recStore storage.RecommendationStore
}

// NewRecommendationServer creates a new RecommendationServer.
func NewRecommendationServer(srv *Server, recSvc *planning.RecommendationService, recStore storage.RecommendationStore) *RecommendationServer {
	return &RecommendationServer{srv: srv, recSvc: recSvc, recStore: recStore}
}

// RegisterRecommendationRoutes registers routes without RBAC.
func (rs *RecommendationServer) RegisterRecommendationRoutes() {
	rs.srv.mux.HandleFunc("/api/v1/recommendations/generate", rs.handleGenerate)
	rs.srv.mux.HandleFunc("/api/v1/recommendations", rs.handleList)
	rs.srv.mux.HandleFunc("/api/v1/recommendations/", rs.handleByID)
}

// RegisterProtectedRecommendationRoutes registers routes with RBAC.
func (rs *RecommendationServer) RegisterProtectedRecommendationRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)
	createMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionUpdate, logger)

	rs.srv.mux.Handle("/api/v1/recommendations/generate", dualMW(createMW(http.HandlerFunc(rs.handleGenerate))))
	rs.srv.mux.Handle("/api/v1/recommendations", dualMW(readMW(http.HandlerFunc(rs.handleList))))
	rs.srv.mux.Handle("/api/v1/recommendations/", dualMW(updateMW(http.HandlerFunc(rs.handleByID))))
}

// handleGenerate generates recommendations for the given pools.
// POST /api/v1/recommendations/generate
func (rs *RecommendationServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rs.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	var req domain.GenerateRecommendationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rs.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if len(req.PoolIDs) == 0 {
		rs.srv.writeErr(r.Context(), w, http.StatusBadRequest, "pool_ids is required", "provide at least one pool ID")
		return
	}

	resp, err := rs.recSvc.Generate(r.Context(), req)
	if err != nil {
		rs.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleList lists recommendations with optional filters.
// GET /api/v1/recommendations
func (rs *RecommendationServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		rs.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use GET")
		return
	}

	q := r.URL.Query()
	filters := domain.RecommendationFilters{
		Type:     q.Get("type"),
		Status:   q.Get("status"),
		Priority: q.Get("priority"),
	}

	if poolIDStr := q.Get("pool_id"); poolIDStr != "" {
		id, err := strconv.ParseInt(poolIDStr, 10, 64)
		if err != nil {
			rs.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid pool_id", err.Error())
			return
		}
		filters.PoolID = id
	}
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			filters.Page = p
		}
	}
	if psStr := q.Get("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil {
			filters.PageSize = ps
		}
	}

	items, total, err := rs.recStore.ListRecommendations(r.Context(), filters)
	if err != nil {
		rs.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	page := filters.Page
	if page < 1 {
		page = 1
	}
	pageSize := filters.PageSize
	if pageSize < 1 {
		pageSize = 50
	}

	writeJSON(w, http.StatusOK, domain.RecommendationsListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// handleByID routes GET/POST for /api/v1/recommendations/{id} and sub-paths.
func (rs *RecommendationServer) handleByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/recommendations/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	if id == "" {
		rs.srv.writeErr(r.Context(), w, http.StatusBadRequest, "recommendation id is required", "")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		rs.handleGet(w, r, id)
	case action == "apply" && r.Method == http.MethodPost:
		rs.handleApply(w, r, id)
	case action == "dismiss" && r.Method == http.MethodPost:
		rs.handleDismiss(w, r, id)
	default:
		rs.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handleGet returns a single recommendation.
// GET /api/v1/recommendations/{id}
func (rs *RecommendationServer) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := rs.recStore.GetRecommendation(r.Context(), id)
	if err != nil {
		rs.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleApply applies a recommendation.
// POST /api/v1/recommendations/{id}/apply
func (rs *RecommendationServer) handleApply(w http.ResponseWriter, r *http.Request, id string) {
	var req domain.ApplyRecommendationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body for compliance recommendations.
		req = domain.ApplyRecommendationRequest{}
	}

	rec, err := rs.recSvc.Apply(r.Context(), id, req)
	if err != nil {
		rs.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleDismiss dismisses a recommendation.
// POST /api/v1/recommendations/{id}/dismiss
func (rs *RecommendationServer) handleDismiss(w http.ResponseWriter, r *http.Request, id string) {
	var req domain.DismissRecommendationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req = domain.DismissRecommendationRequest{}
	}

	rec, err := rs.recSvc.Dismiss(r.Context(), id, req.Reason)
	if err != nil {
		rs.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}
