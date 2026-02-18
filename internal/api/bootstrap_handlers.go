package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// handleProvisionAgent handles POST /api/v1/discovery/agents/provision
// Admin creates a provisioning bundle (API key + config) for a new agent.
// Returns the plaintext API key and a base64-encoded JSON token the agent can use.
func (d *DiscoveryServer) handleProvisionAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.AgentProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "name is required", "")
		return
	}

	if d.keyStore == nil {
		d.srv.writeErr(r.Context(), w, http.StatusServiceUnavailable, "key store not available", "")
		return
	}

	// Generate an API key scoped for discovery operations
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   fmt.Sprintf("agent:%s", req.Name),
		Scopes: []string{"discovery:write"},
	})
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to generate api key", err.Error())
		return
	}

	if err := d.keyStore.Create(r.Context(), apiKey); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to store api key", err.Error())
		return
	}

	// Determine server URL
	serverURL := os.Getenv("CLOUDPAM_SERVER_URL")
	if serverURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		serverURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	// Build the provisioning bundle
	bundle := domain.AgentProvisionBundle{
		AgentName: req.Name,
		APIKey:    plaintext,
		ServerURL: serverURL,
	}

	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to encode bundle", err.Error())
		return
	}
	token := base64.StdEncoding.EncodeToString(bundleJSON)

	resp := domain.AgentProvisionResponse{
		AgentName: req.Name,
		APIKey:    plaintext,
		APIKeyID:  apiKey.ID,
		ServerURL: serverURL,
		Token:     token,
	}

	writeJSON(w, http.StatusCreated, resp)
}

// handleAgentRegister handles POST /api/v1/discovery/agents/register
// Authenticated via API key (provisioned by the admin).
// The agent calls this on first startup with its auto-generated agent_id
// and cloud-derived account_id.
func (d *DiscoveryServer) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.AgentRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.AccountID < 1 || req.AgentID == uuid.Nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "name, account_id, and agent_id are required", "")
		return
	}

	// Get API key ID from context (proves the agent is authenticated)
	apiKeyID, ok := getAPIKeyIDFromContext(r.Context())
	if !ok {
		d.srv.writeErr(r.Context(), w, http.StatusUnauthorized, "api key required", "")
		return
	}

	// Verify account exists
	_, found, err := d.srv.store.GetAccount(r.Context(), req.AccountID)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "account lookup failed", err.Error())
		return
	}
	if !found {
		d.srv.writeErr(r.Context(), w, http.StatusNotFound, "account not found", "")
		return
	}

	// Create the agent record
	now := time.Now().UTC()
	agent := domain.DiscoveryAgent{
		ID:             req.AgentID,
		Name:           req.Name,
		AccountID:      req.AccountID,
		APIKeyID:       apiKeyID,
		ApprovalStatus: domain.AgentApprovalApproved, // pre-approved via provisioned API key
		Version:        req.Version,
		Hostname:       req.Hostname,
		LastSeenAt:     now,
		RegisteredAt:   &now,
		ApprovedAt:     &now,
		CreatedAt:      now,
	}
	approvedBy := "provisioned"
	agent.ApprovedBy = &approvedBy

	if err := d.store.UpsertAgent(r.Context(), agent); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to create agent", err.Error())
		return
	}

	resp := domain.AgentRegisterResponse{
		AgentID:        req.AgentID,
		ApprovalStatus: domain.AgentApprovalApproved,
		Message:        "agent registered",
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleApproveAgent handles POST /api/v1/discovery/agents/{id}/approve
func (d *DiscoveryServer) handleApproveAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/agents/")
	idStr = strings.TrimSuffix(idStr, "/approve")
	agentID, err := uuid.Parse(idStr)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid agent id", "")
		return
	}

	agent, err := d.store.GetAgent(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			d.srv.writeErr(r.Context(), w, http.StatusNotFound, "agent not found", "")
			return
		}
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "get agent failed", err.Error())
		return
	}

	if agent.ApprovalStatus == domain.AgentApprovalApproved {
		d.srv.writeErr(r.Context(), w, http.StatusConflict, "agent is already approved", "")
		return
	}

	now := time.Now().UTC()
	agent.ApprovalStatus = domain.AgentApprovalApproved
	agent.ApprovedAt = &now

	approvedBy := "anonymous"
	if user := auth.UserFromContext(r.Context()); user != nil {
		approvedBy = user.Username
	} else if key := auth.APIKeyFromContext(r.Context()); key != nil {
		approvedBy = "apikey:" + key.Name
	}
	agent.ApprovedBy = &approvedBy

	if err := d.store.UpsertAgent(r.Context(), *agent); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to update agent", err.Error())
		return
	}

	resp := domain.AgentRegisterResponse{
		AgentID:        agentID,
		ApprovalStatus: domain.AgentApprovalApproved,
		Message:        "agent approved",
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRejectAgent handles POST /api/v1/discovery/agents/{id}/reject
func (d *DiscoveryServer) handleRejectAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/agents/")
	idStr = strings.TrimSuffix(idStr, "/reject")
	agentID, err := uuid.Parse(idStr)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid agent id", "")
		return
	}

	agent, err := d.store.GetAgent(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			d.srv.writeErr(r.Context(), w, http.StatusNotFound, "agent not found", "")
			return
		}
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "get agent failed", err.Error())
		return
	}

	if agent.ApprovalStatus == domain.AgentApprovalRejected {
		d.srv.writeErr(r.Context(), w, http.StatusConflict, "agent is already rejected", "")
		return
	}

	agent.ApprovalStatus = domain.AgentApprovalRejected

	if err := d.store.UpsertAgent(r.Context(), *agent); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to update agent", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":        agentID,
		"approval_status": domain.AgentApprovalRejected,
		"message":         "agent rejected",
	})
}

// handleAgentsSubroutes dispatches /api/v1/discovery/agents/ subroutes.
// Handles: /{id}, /{id}/approve, /{id}/reject, /register, /provision, /heartbeat
func (d *DiscoveryServer) handleAgentsSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/agents/")
	path = strings.TrimRight(path, "/")

	switch {
	case path == "register":
		d.handleAgentRegister(w, r)
	case path == "provision":
		d.handleProvisionAgent(w, r)
	case path == "heartbeat":
		d.handleAgentHeartbeat(w, r)
	case strings.HasSuffix(path, "/approve"):
		d.handleApproveAgent(w, r)
	case strings.HasSuffix(path, "/reject"):
		d.handleRejectAgent(w, r)
	default:
		d.handleGetAgent(w, r)
	}
}
