package http

import (
	"encoding/base64"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func setupDiscoveryTestServer() (*DiscoveryServer, *storage.MemoryStore, *storage.MemoryDiscoveryStore, *auth.MemoryKeyStore) {
	// Use the same MemoryStore for both Server and DiscoveryStore
	srv, st := setupTestServer()
	ds := storage.NewMemoryDiscoveryStore(st)
	ks := auth.NewMemoryKeyStore()
	syncSvc := discovery.NewSyncService(ds)
	discSrv := NewDiscoveryServer(srv, ds, syncSvc, ks)
	discSrv.RegisterDiscoveryRoutes()
	return discSrv, st, ds, ks
}

func TestProvisionAgent(t *testing.T) {
	discSrv, _, _, ks := setupDiscoveryTestServer()

	// Provision an agent
	rr := doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/provision",
		`{"name":"us-east-agent"}`,
		stdhttp.StatusCreated)

	var resp domain.AgentProvisionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.AgentName != "us-east-agent" {
		t.Fatalf("expected agent_name us-east-agent, got %s", resp.AgentName)
	}
	if resp.APIKey == "" {
		t.Fatal("expected api_key to be returned")
	}
	if resp.APIKeyID == "" {
		t.Fatal("expected api_key_id to be returned")
	}
	if resp.ServerURL == "" {
		t.Fatal("expected server_url to be set")
	}
	if resp.Token == "" {
		t.Fatal("expected base64 token to be returned")
	}

	// Verify the token decodes to a valid bundle
	bundleJSON, err := base64.StdEncoding.DecodeString(resp.Token)
	if err != nil {
		t.Fatalf("decode base64 token: %v", err)
	}
	var bundle domain.AgentProvisionBundle
	if err := json.Unmarshal(bundleJSON, &bundle); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}
	if bundle.AgentName != "us-east-agent" {
		t.Fatalf("bundle agent_name mismatch: %s", bundle.AgentName)
	}
	if bundle.APIKey != resp.APIKey {
		t.Fatal("bundle api_key should match response api_key")
	}
	if bundle.ServerURL != resp.ServerURL {
		t.Fatal("bundle server_url should match response server_url")
	}

	// Verify the API key was stored
	keys, _ := ks.List(t.Context())
	found := false
	for _, k := range keys {
		if k.Name == "agent:us-east-agent" {
			found = true
			if len(k.Scopes) != 1 || k.Scopes[0] != "discovery:write" {
				t.Fatalf("expected scopes [discovery:write], got %v", k.Scopes)
			}
			break
		}
	}
	if !found {
		t.Fatal("api key for agent should exist in key store")
	}
}

func TestProvisionAgent_Validation(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	// Missing name
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/provision",
		`{"name":""}`, stdhttp.StatusBadRequest)

	// Empty body
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/provision",
		`{}`, stdhttp.StatusBadRequest)

	// Wrong method
	doJSON(t, discSrv.srv.mux, stdhttp.MethodGet, "/api/v1/discovery/agents/provision",
		"", stdhttp.StatusMethodNotAllowed)
}

func TestAgentRegister(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()

	// Create an account first
	if _, err := st.CreateAccount(t.Context(), domain.CreateAccount{Name: "aws-prod", Key: "aws-prod"}); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Provision an agent to get an API key
	rr := doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/provision",
		`{"name":"my-agent"}`, stdhttp.StatusCreated)

	var provResp domain.AgentProvisionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &provResp); err != nil {
		t.Fatalf("unmarshal provision response: %v", err)
	}

	// The register endpoint checks getAPIKeyIDFromContext, which requires the auth
	// middleware to have run. In unprotected mode (dev), the handler checks directly.
	// For this test, we simulate an authenticated request by injecting the API key
	// into the context.
	agentID := uuid.New()
	reqBody := `{"agent_id":"` + agentID.String() + `","name":"my-agent","account_id":1,"version":"1.0.0","hostname":"ip-10-0-1-42"}`

	// In dev mode (no auth middleware), the register handler checks getAPIKeyIDFromContext
	// which will return empty. The handler returns 401.
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/register",
		reqBody, stdhttp.StatusUnauthorized)

	// Simulate authenticated request by creating a context with the API key
	apiKey, _ := discSrv.keyStore.GetByID(t.Context(), provResp.APIKeyID)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/discovery/agents/register",
		strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.ContextWithAPIKey(req.Context(), apiKey)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	discSrv.srv.mux.ServeHTTP(w, req)

	if w.Code != stdhttp.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var regResp domain.AgentRegisterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if regResp.AgentID != agentID {
		t.Fatalf("expected agent_id %s, got %s", agentID, regResp.AgentID)
	}
	if regResp.ApprovalStatus != domain.AgentApprovalApproved {
		t.Fatalf("expected approved, got %s", regResp.ApprovalStatus)
	}

	// Verify agent was stored
	agent, err := ds.GetAgent(t.Context(), agentID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.Name != "my-agent" {
		t.Fatalf("expected name my-agent, got %s", agent.Name)
	}
	if agent.AccountID != 1 {
		t.Fatalf("expected account_id 1, got %d", agent.AccountID)
	}
	if agent.APIKeyID != provResp.APIKeyID {
		t.Fatalf("expected api_key_id %s, got %s", provResp.APIKeyID, agent.APIKeyID)
	}
	if agent.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", agent.Version)
	}
}

func TestAgentRegister_Validation(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	// Missing agent_id
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/register",
		`{"name":"agent","account_id":1}`, stdhttp.StatusBadRequest)

	// Missing name
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/register",
		`{"agent_id":"`+uuid.New().String()+`","account_id":1}`, stdhttp.StatusBadRequest)

	// Missing account_id
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/register",
		`{"agent_id":"`+uuid.New().String()+`","name":"agent"}`, stdhttp.StatusBadRequest)
}

func TestAgentRegister_AccountNotFound(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	// Provision to get an API key
	rr := doJSON(t, discSrv.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/agents/provision",
		`{"name":"agent"}`, stdhttp.StatusCreated)
	var provResp domain.AgentProvisionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &provResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Register with non-existent account (with API key in context)
	apiKey, _ := discSrv.keyStore.GetByID(t.Context(), provResp.APIKeyID)
	reqBody := `{"agent_id":"` + uuid.New().String() + `","name":"agent","account_id":999}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/discovery/agents/register",
		strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithAPIKey(req.Context(), apiKey))

	w := httptest.NewRecorder()
	discSrv.srv.mux.ServeHTTP(w, req)

	if w.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentApproveReject(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()

	// Create an agent directly in pending state
	agentID := uuid.New()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID:             agentID,
		Name:           "pending-agent",
		AccountID:      1,
		ApprovalStatus: domain.AgentApprovalPending,
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	// Approve agent
	rr := doJSON(t, discSrv.srv.mux, stdhttp.MethodPost,
		"/api/v1/discovery/agents/"+agentID.String()+"/approve", "",
		stdhttp.StatusOK)

	var resp domain.AgentRegisterResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ApprovalStatus != domain.AgentApprovalApproved {
		t.Fatalf("expected approved, got %s", resp.ApprovalStatus)
	}

	// Approve again → conflict
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost,
		"/api/v1/discovery/agents/"+agentID.String()+"/approve", "",
		stdhttp.StatusConflict)

	// Create another agent, reject it
	agentID2 := uuid.New()
	if err := ds.UpsertAgent(t.Context(), domain.DiscoveryAgent{
		ID:             agentID2,
		Name:           "reject-agent",
		AccountID:      1,
		ApprovalStatus: domain.AgentApprovalPending,
	}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost,
		"/api/v1/discovery/agents/"+agentID2.String()+"/reject", "",
		stdhttp.StatusOK)

	// Reject again → conflict
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost,
		"/api/v1/discovery/agents/"+agentID2.String()+"/reject", "",
		stdhttp.StatusConflict)

	// Non-existent agent
	doJSON(t, discSrv.srv.mux, stdhttp.MethodPost,
		"/api/v1/discovery/agents/"+uuid.New().String()+"/approve", "",
		stdhttp.StatusNotFound)
}
