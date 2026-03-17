package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// DriftServer handles drift detection API endpoints.
type DriftServer struct {
	srv        *Server
	detector   *discovery.DriftDetector
	driftStore storage.DriftStore
}

// NewDriftServer creates a new DriftServer.
func NewDriftServer(srv *Server, detector *discovery.DriftDetector, driftStore storage.DriftStore) *DriftServer {
	return &DriftServer{srv: srv, detector: detector, driftStore: driftStore}
}

// RegisterProtectedDriftRoutes registers drift routes with RBAC.
func (ds *DriftServer) RegisterProtectedDriftRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	createMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionCreate, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionUpdate, logger)

	ds.srv.mux.Handle("/api/v1/drift/detect", dualMW(createMW(http.HandlerFunc(ds.handleDetect))))
	ds.srv.mux.Handle("/api/v1/drift", dualMW(readMW(http.HandlerFunc(ds.handleList))))
	ds.srv.mux.Handle("/api/v1/drift/", dualMW(updateMW(http.HandlerFunc(ds.handleByID))))
}

// handleDetect runs drift detection.
// POST /api/v1/drift/detect
func (ds *DriftServer) handleDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ds.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	var req domain.RunDriftDetectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body — detect all accounts.
		req = domain.RunDriftDetectionRequest{}
	}

	resp, err := ds.detector.Detect(r.Context(), req)
	if err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleList lists drift items with optional filters.
// GET /api/v1/drift
func (ds *DriftServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ds.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use GET")
		return
	}

	q := r.URL.Query()
	filters := domain.DriftFilters{
		Type:     q.Get("type"),
		Severity: q.Get("severity"),
		Status:   q.Get("status"),
	}

	if idStr := q.Get("account_id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			ds.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid account_id", err.Error())
			return
		}
		filters.AccountID = id
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

	items, total, err := ds.driftStore.ListDriftItems(r.Context(), filters)
	if err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
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

	// Build summary from current items.
	summary := domain.DriftSummary{
		BySeverity: map[string]int{},
		ByType:     map[string]int{},
	}
	// Get all items for summary (unfiltered by page).
	allItems, allTotal, _ := ds.driftStore.ListDriftItems(r.Context(), domain.DriftFilters{
		AccountID: filters.AccountID,
		Status:    filters.Status,
		Page:      1,
		PageSize:  10000,
	})
	summary.TotalDrifts = allTotal
	for _, item := range allItems {
		summary.BySeverity[string(item.Severity)]++
		summary.ByType[string(item.Type)]++
	}

	writeJSON(w, http.StatusOK, domain.DriftListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Summary:  summary,
	})
}

// handleByID routes GET/POST for /api/v1/drift/{id} and sub-paths.
func (ds *DriftServer) handleByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/drift/")
	// Avoid matching /api/v1/drift/detect here.
	if path == "detect" {
		ds.handleDetect(w, r)
		return
	}

	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	if id == "" {
		ds.srv.writeErr(r.Context(), w, http.StatusBadRequest, "drift item id is required", "")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		ds.handleGet(w, r, id)
	case action == "resolve" && r.Method == http.MethodPost:
		ds.handleResolve(w, r, id)
	case action == "ignore" && r.Method == http.MethodPost:
		ds.handleIgnore(w, r, id)
	default:
		ds.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handleGet returns a single drift item.
// GET /api/v1/drift/{id}
func (ds *DriftServer) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	item, err := ds.driftStore.GetDriftItem(r.Context(), id)
	if err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleResolve marks a drift item as resolved.
// POST /api/v1/drift/{id}/resolve
func (ds *DriftServer) handleResolve(w http.ResponseWriter, r *http.Request, id string) {
	if err := ds.driftStore.UpdateDriftStatus(r.Context(), id, domain.DriftStatusResolved, ""); err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	item, err := ds.driftStore.GetDriftItem(r.Context(), id)
	if err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleIgnore marks a drift item as ignored.
// POST /api/v1/drift/{id}/ignore
func (ds *DriftServer) handleIgnore(w http.ResponseWriter, r *http.Request, id string) {
	var req domain.IgnoreDriftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req = domain.IgnoreDriftRequest{}
	}

	if err := ds.driftStore.UpdateDriftStatus(r.Context(), id, domain.DriftStatusIgnored, req.Reason); err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	item, err := ds.driftStore.GetDriftItem(r.Context(), id)
	if err != nil {
		ds.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
