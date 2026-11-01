package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/storage"
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
	a.srv.handleOpenAPIRouteFunc("/api/v1/ai/chat", a.handleChat)
	a.srv.handleOpenAPIRouteFunc("/api/v1/ai/sessions", a.handleSessions)
	a.srv.handleOpenAPIRouteFunc("/api/v1/ai/sessions/", a.handleSessionByID)
}

// RegisterProtectedAIPlanningRoutes registers routes with RBAC.
func (a *AIPlanningServer) RegisterProtectedAIPlanningRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)
	createMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionUpdate, logger)

	a.srv.handleOpenAPIRoute("/api/v1/ai/chat", dualMW(readMW(http.HandlerFunc(a.handleChat))))
	a.srv.handleOpenAPIRoute("/api/v1/ai/sessions", dualMW(readMW(http.HandlerFunc(a.handleSessions))))
	a.srv.handleOpenAPIRoute("POST /api/v1/ai/sessions/{id}/apply-plan", dualMW(createMW(http.HandlerFunc(a.handleSessionByID))))
	// sessions/ handles GET and DELETE for individual planning sessions.
	a.srv.handleOpenAPIRoute("/api/v1/ai/sessions/", dualMW(updateMW(http.HandlerFunc(a.handleSessionByID))))
}

// aiUnavailableDetail tells an operator how to turn AI planning on.
const aiUnavailableDetail = "configure a provider under Configuration → AI Provider, or set CLOUDPAM_LLM_API_KEY / CLOUDPAM_LLM_ENDPOINT"

func (a *AIPlanningServer) writeUnavailable(w http.ResponseWriter, r *http.Request) {
	a.srv.writeErr(r.Context(), w, http.StatusServiceUnavailable, "ai planning not available", aiUnavailableDetail)
}

// handleChat streams an SSE response for a chat message.
// POST /api/v1/ai/chat
func (a *AIPlanningServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "use POST")
		return
	}

	if !a.aiSvc.Available(r.Context()) {
		a.writeUnavailable(w, r)
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
	if errors.Is(err, planning.ErrAIUnavailable) {
		a.writeUnavailable(w, r)
		return
	}
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
	_ = rc.SetWriteDeadline(time.Now().Add(aiChatStreamTimeout))

	// Flush through the ResponseController rather than a direct type assertion:
	// middleware wraps the ResponseWriter, and a wrapper that only forwards
	// Unwrap would fail a plain w.(http.Flusher) assertion and kill streaming.
	if err := rc.Flush(); err != nil {
		a.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "streaming not supported", err.Error())
		return
	}

	for evt := range eventCh {
		if evt.Err != nil {
			// The provider failed mid-stream (or the stream exceeded its cap).
			// Tell the client instead of ending the answer as if complete; the
			// message is sanitized and never carries the provider's body.
			data, _ := json.Marshal(map[string]string{"error": llm.DiagnoseError(evt.Err, aiChatStreamTimeout)})
			_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
			_ = rc.Flush()
			return
		}
		// The done event can carry the last token when the provider puts it
		// in the finish_reason chunk; send it before closing the stream so the
		// client sees the same reply that was stored.
		if evt.Delta != "" || !evt.Done {
			data, _ := json.Marshal(map[string]string{"delta": evt.Delta})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
		if evt.Done {
			_, _ = fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			_ = rc.Flush()
			break
		}
		_ = rc.Flush()
	}
}

// aiChatStreamTimeout is the write deadline of one chat stream and the budget
// reported when a stream times out.
const aiChatStreamTimeout = 5 * time.Minute

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

	pools := make([]schemaPoolEntry, len(req.Plan.Pools))
	for i, p := range req.Plan.Pools {
		pools[i] = schemaPoolEntry(p)
	}
	a.srv.applyPoolPlan(ctx, w, poolPlan{
		Pools:         pools,
		SkipConflicts: req.SkipConflicts,
		Status:        domain.PoolStatusPlanned,
		Description:   "Created by AI Planner",
		Tags:          map[string]string{"ai_planner": "true", "session_id": sessionID},
	})
}
