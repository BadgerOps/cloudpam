package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"cloudpam/internal/auth"
	"cloudpam/internal/planning"
)

// AnalysisServer handles analysis API endpoints.
type AnalysisServer struct {
	srv      *Server
	analysis *planning.AnalysisService
}

// NewAnalysisServer creates a new AnalysisServer.
func NewAnalysisServer(srv *Server, analysis *planning.AnalysisService) *AnalysisServer {
	return &AnalysisServer{srv: srv, analysis: analysis}
}

// RegisterAnalysisRoutes registers analysis routes without RBAC.
func (a *AnalysisServer) RegisterAnalysisRoutes() {
	a.srv.mux.HandleFunc("/api/v1/analysis", a.handleAnalysis)
	a.srv.mux.HandleFunc("/api/v1/analysis/gaps", a.handleGaps)
	a.srv.mux.HandleFunc("/api/v1/analysis/fragmentation", a.handleFragmentation)
	a.srv.mux.HandleFunc("/api/v1/analysis/compliance", a.handleCompliance)
}

// RegisterProtectedAnalysisRoutes registers analysis routes with RBAC.
func (a *AnalysisServer) RegisterProtectedAnalysisRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)

	a.srv.mux.Handle("/api/v1/analysis", dualMW(readMW(http.HandlerFunc(a.handleAnalysis))))
	a.srv.mux.Handle("/api/v1/analysis/gaps", dualMW(readMW(http.HandlerFunc(a.handleGaps))))
	a.srv.mux.Handle("/api/v1/analysis/fragmentation", dualMW(readMW(http.HandlerFunc(a.handleFragmentation))))
	a.srv.mux.Handle("/api/v1/analysis/compliance", dualMW(readMW(http.HandlerFunc(a.handleCompliance))))
}

// handleAnalysis runs a full analysis report.
// POST /api/v1/analysis
func (a *AnalysisServer) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	var req planning.AnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	report, err := a.analysis.Analyze(r.Context(), req)
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// handleGaps runs gap analysis for a single pool.
// POST /api/v1/analysis/gaps
func (a *AnalysisServer) handleGaps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	var req planning.GapAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.PoolID == 0 {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "pool_id is required", "")
		return
	}

	result, err := a.analysis.AnalyzeGaps(r.Context(), req.PoolID)
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleFragmentation runs fragmentation analysis.
// POST /api/v1/analysis/fragmentation
func (a *AnalysisServer) handleFragmentation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	var req planning.AnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if len(req.PoolIDs) == 0 {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "pool_ids is required", "provide at least one pool ID")
		return
	}

	result, err := a.analysis.AnalyzeFragmentation(r.Context(), req.PoolIDs[0])
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleCompliance runs compliance checks.
// POST /api/v1/analysis/compliance
func (a *AnalysisServer) handleCompliance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	var req planning.AnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	result, err := a.analysis.CheckCompliance(r.Context(), req.PoolIDs, req.IncludeChildren)
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
