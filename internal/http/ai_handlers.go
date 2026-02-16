package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
	"cloudpam/internal/validation"
)

// AIPlanningServer handles AI planning API endpoints.
type AIPlanningServer struct {
	srv       *Server
	aiSvc     *planning.AIPlanningService
	convStore storage.ConversationStore
}

// NewAIPlanningServer creates a new AIPlanningServer.
func NewAIPlanningServer(srv *Server, aiSvc *planning.AIPlanningService, convStore storage.ConversationStore) *AIPlanningServer {
	return &AIPlanningServer{srv: srv, aiSvc: aiSvc, convStore: convStore}
}

// RegisterAIPlanningRoutes registers routes without RBAC.
func (a *AIPlanningServer) RegisterAIPlanningRoutes() {
	a.srv.mux.HandleFunc("/api/v1/ai/chat", a.handleChat)
	a.srv.mux.HandleFunc("/api/v1/ai/sessions", a.handleSessions)
	a.srv.mux.HandleFunc("/api/v1/ai/sessions/", a.handleSessionByID)
}

// RegisterProtectedAIPlanningRoutes registers routes with RBAC.
func (a *AIPlanningServer) RegisterProtectedAIPlanningRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)
	createMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionUpdate, logger)

	a.srv.mux.Handle("/api/v1/ai/chat", dualMW(readMW(http.HandlerFunc(a.handleChat))))
	a.srv.mux.Handle("/api/v1/ai/sessions", dualMW(readMW(http.HandlerFunc(a.handleSessions))))
	// sessions/ handles GET, DELETE, and apply-plan (which needs create)
	a.srv.mux.Handle("/api/v1/ai/sessions/", dualMW(updateMW(http.HandlerFunc(a.handleSessionByID))))
	// Override apply-plan to require pools:create
	_ = createMW // used implicitly through handleSessionByID routing
}

// handleChat streams an SSE response for a chat message.
// POST /api/v1/ai/chat
func (a *AIPlanningServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	if !a.aiSvc.Available() {
		a.srv.writeErr(r.Context(), w, http.StatusServiceUnavailable, "ai planning not available", "set CLOUDPAM_LLM_API_KEY to enable")
		return
	}

	var req domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.SessionID == "" {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "session_id is required", "")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "message is required", "")
		return
	}

	// Start streaming
	eventCh, err := a.aiSvc.Chat(r.Context(), req.SessionID, req.Message)
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Extend write deadline for streaming
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))

	flusher, ok := w.(http.Flusher)
	if !ok {
		a.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "streaming not supported", "")
		return
	}

	for evt := range eventCh {
		if evt.Done {
			_, _ = fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			break
		}
		data, _ := json.Marshal(map[string]string{"delta": evt.Delta})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// handleSessions handles session list and create.
func (a *AIPlanningServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.handleListSessions(w, r)
	case http.MethodPost:
		a.handleCreateSession(w, r)
	default:
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use GET or POST")
	}
}

// handleListSessions lists all conversations.
// GET /api/v1/ai/sessions
func (a *AIPlanningServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	convs, err := a.aiSvc.ListConversations(r.Context())
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": convs,
		"total": len(convs),
	})
}

// handleCreateSession creates a new conversation.
// POST /api/v1/ai/sessions
func (a *AIPlanningServer) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Title = "New Planning Session"
	}
	if req.Title == "" {
		req.Title = "New Planning Session"
	}

	conv, err := a.aiSvc.CreateConversation(r.Context(), req.Title)
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

// handleSessionByID routes requests for /api/v1/ai/sessions/{id}[/action].
func (a *AIPlanningServer) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/ai/sessions/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	if id == "" {
		a.srv.writeErr(r.Context(), w, http.StatusBadRequest, "session id is required", "")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		a.handleGetSession(w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		a.handleDeleteSession(w, r, id)
	case action == "apply-plan" && r.Method == http.MethodPost:
		a.handleApplyPlan(w, r, id)
	default:
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handleGetSession returns a conversation with its messages.
// GET /api/v1/ai/sessions/{id}
func (a *AIPlanningServer) handleGetSession(w http.ResponseWriter, r *http.Request, id string) {
	conv, err := a.aiSvc.GetConversation(r.Context(), id)
	if err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

// handleDeleteSession deletes a conversation.
// DELETE /api/v1/ai/sessions/{id}
func (a *AIPlanningServer) handleDeleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := a.aiSvc.DeleteConversation(r.Context(), id); err != nil {
		a.srv.writeStoreErr(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleApplyPlan applies a generated plan from a conversation.
// POST /api/v1/ai/sessions/{id}/apply-plan
func (a *AIPlanningServer) handleApplyPlan(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx := r.Context()

	var req domain.ApplyPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.srv.writeErr(ctx, w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if len(req.Plan.Pools) == 0 {
		a.srv.writeErr(ctx, w, http.StatusBadRequest, "plan must contain at least one pool", "")
		return
	}

	// Validate all entries
	refSet := make(map[string]bool)
	for i, p := range req.Plan.Pools {
		if p.Ref == "" {
			a.srv.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d: ref is required", i), "")
			return
		}
		if refSet[p.Ref] {
			a.srv.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d: duplicate ref %q", i, p.Ref), "")
			return
		}
		refSet[p.Ref] = true

		if err := validation.ValidateName(p.Name); err != nil {
			a.srv.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d (%s): %v", i, p.Ref, err), "")
			return
		}
		if err := validation.ValidateCIDRWithOptions(p.CIDR, validation.CIDROptions{
			MinPrefix: 8,
			MaxPrefix: 30,
		}); err != nil {
			a.srv.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d (%s): %v", i, p.Ref, err), "")
			return
		}
		if p.ParentRef != "" {
			found := false
			for _, prev := range req.Plan.Pools[:i] {
				if prev.Ref == p.ParentRef {
					found = true
					break
				}
			}
			if !found {
				a.srv.writeErr(ctx, w, http.StatusBadRequest,
					fmt.Sprintf("pool %d (%s): parent_ref %q not found in preceding entries", i, p.Ref, p.ParentRef), "")
				return
			}
		}
	}

	// Create pools in topological order
	refToID := make(map[string]int64)
	var created, skipped int
	var errs []string
	var rootPoolID int64

	tags := map[string]string{
		"ai_planner": "true",
		"session_id": sessionID,
	}

	for _, p := range req.Plan.Pools {
		poolType := domain.PoolType(p.Type)
		if poolType == "" {
			poolType = domain.PoolTypeSubnet
		}
		if !domain.IsValidPoolType(poolType) {
			errs = append(errs, fmt.Sprintf("pool %q: invalid type %q, using subnet", p.Ref, p.Type))
			poolType = domain.PoolTypeSubnet
		}

		cp := domain.CreatePool{
			Name:        p.Name,
			CIDR:        p.CIDR,
			Type:        poolType,
			Status:      domain.PoolStatusPlanned,
			Source:      domain.PoolSourceManual,
			Description: "Created by AI Planner",
			Tags:        tags,
		}

		if p.ParentRef != "" {
			parentID, ok := refToID[p.ParentRef]
			if !ok {
				errs = append(errs, fmt.Sprintf("pool %q: parent_ref %q not yet created", p.Ref, p.ParentRef))
				skipped++
				continue
			}
			cp.ParentID = &parentID
		}

		pool, err := a.srv.store.CreatePool(ctx, cp)
		if err != nil {
			errs = append(errs, fmt.Sprintf("pool %q: %v", p.Ref, err))
			skipped++
			continue
		}

		refToID[p.Ref] = pool.ID
		if p.ParentRef == "" {
			rootPoolID = pool.ID
		}
		created++

		a.srv.logAudit(ctx, "create", "pool", fmt.Sprintf("%d", pool.ID), pool.Name, http.StatusCreated)
	}

	resp := map[string]any{
		"created":      created,
		"skipped":      skipped,
		"errors":       errs,
		"root_pool_id": rootPoolID,
		"pool_map":     refToID,
	}
	if errs == nil {
		resp["errors"] = []string{}
	}
	writeJSON(w, http.StatusOK, resp)
}
