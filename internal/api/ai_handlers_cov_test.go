package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/planning"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/storage"
)

// stubLLMProviderCov is a hermetic LLM provider: it never performs any network
// I/O and emits a fixed sequence of stream events.
type stubLLMProviderCov struct {
	available  bool
	deltas     []string
	streamErr  error
	emitDone   bool
	lastPrompt []llm.Message
}

func (p *stubLLMProviderCov) Name() string { return "stub" }

func (p *stubLLMProviderCov) Available() bool { return p.available }

func (p *stubLLMProviderCov) Complete(_ context.Context, _ []llm.Message, _ llm.Options) (*llm.Response, error) {
	return &llm.Response{Content: strings.Join(p.deltas, "")}, nil
}

func (p *stubLLMProviderCov) StreamComplete(_ context.Context, messages []llm.Message, _ llm.Options) (<-chan llm.StreamEvent, error) {
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	p.lastPrompt = messages
	ch := make(chan llm.StreamEvent, len(p.deltas)+1)
	for _, d := range p.deltas {
		ch <- llm.StreamEvent{Delta: d}
	}
	if p.emitDone {
		ch <- llm.StreamEvent{Done: true, FinishReason: "stop"}
	}
	close(ch)
	return ch, nil
}

// setupAITestServerCov wires an AIPlanningServer over in-memory stores and the
// stub provider, registering the unprotected routes.
func setupAITestServerCov(t *testing.T, provider llm.Provider) (*AIPlanningServer, *storage.MemoryStore, *storage.MemoryConversationStore) {
	t.Helper()
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)

	convStore := storage.NewMemoryConversationStore(st)
	aiSvc := planning.NewAIPlanningService(planning.NewAnalysisService(st), convStore, st, provider)
	ai := NewAIPlanningServer(srv, aiSvc, convStore)
	ai.RegisterAIPlanningRoutes()
	return ai, st, convStore
}

// newConversationCov seeds a conversation and returns its ID.
func newConversationCov(t *testing.T, store *storage.MemoryConversationStore, title string) string {
	t.Helper()
	id := uuid.New().String()
	now := time.Now().UTC()
	if err := store.CreateConversation(t.Context(), domain.Conversation{
		ID: id, Title: title, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	return id
}

func TestAISessionsCRUDCov(t *testing.T) {
	ai, _, convStore := setupAITestServerCov(t, &stubLLMProviderCov{available: true})
	mux := ai.srv.mux

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPut, "/api/v1/ai/sessions", ""), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/ai/sessions", ""), http.StatusOK)
		var resp struct {
			Items []domain.Conversation `json:"items"`
			Total int                   `json:"total"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Total != 0 || len(resp.Items) != 0 {
			t.Fatalf("expected an empty listing, got %+v", resp)
		}
	})

	var created domain.Conversation
	t.Run("create with title", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/ai/sessions", `{"title":"Datacenter plan"}`), http.StatusCreated)
		if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if created.ID == "" || created.Title != "Datacenter plan" {
			t.Fatalf("unexpected conversation: %+v", created)
		}
	})

	t.Run("create defaults the title", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"title":""}`, `not json at all`} {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/ai/sessions", body), http.StatusCreated)
			var conv domain.Conversation
			if err := json.Unmarshal(rr.Body.Bytes(), &conv); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if conv.Title != "New Planning Session" {
				t.Fatalf("body %q: title = %q, want the default", body, conv.Title)
			}
		}
	})

	t.Run("list reflects created sessions", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/ai/sessions", ""), http.StatusOK)
		var resp struct {
			Items []domain.Conversation `json:"items"`
			Total int                   `json:"total"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Total != 4 || len(resp.Items) != 4 {
			t.Fatalf("total = %d, len = %d, want 4", resp.Total, len(resp.Items))
		}
	})

	t.Run("get session", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/ai/sessions/"+created.ID, ""), http.StatusOK)
		var conv domain.ConversationWithMessages
		if err := json.Unmarshal(rr.Body.Bytes(), &conv); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if conv.ID != created.ID {
			t.Fatalf("id = %q, want %q", conv.ID, created.ID)
		}
	})

	t.Run("get unknown session", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/ai/sessions/does-not-exist", ""), http.StatusNotFound)
	})

	t.Run("missing session id", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/ai/sessions/", ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "session id is required" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("unsupported action", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/ai/sessions/"+created.ID, `{}`), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("delete session", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/ai/sessions/"+created.ID, ""), http.StatusOK)
		var body map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body["status"] != "deleted" {
			t.Fatalf("status = %q", body["status"])
		}
		if _, err := convStore.GetConversation(t.Context(), created.ID); err == nil {
			t.Fatal("conversation should be gone")
		}
	})

	t.Run("delete unknown session", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/ai/sessions/nope", ""), http.StatusNotFound)
	})
}

func TestAIChatUnavailableCov(t *testing.T) {
	ai, _, _ := setupAITestServerCov(t, &stubLLMProviderCov{available: false})
	rr := assertStatusCov(t, doReqCov(t, ai.srv.mux, http.MethodPost, "/api/v1/ai/chat",
		`{"session_id":"s1","message":"hi"}`), http.StatusServiceUnavailable)
	if e := decodeErrCov(t, rr); e.Error != "ai planning not available" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestAIChatValidationCov(t *testing.T) {
	ai, _, _ := setupAITestServerCov(t, &stubLLMProviderCov{available: true})
	mux := ai.srv.mux

	tests := []struct {
		name     string
		method   string
		body     string
		wantCode int
		wantErr  string
	}{
		{"method not allowed", http.MethodGet, "", http.StatusMethodNotAllowed, "method not allowed"},
		{"malformed json", http.MethodPost, `{`, http.StatusBadRequest, "invalid request body"},
		{"missing session id", http.MethodPost, `{"message":"hi"}`, http.StatusBadRequest, "session_id is required"},
		{"blank message", http.MethodPost, `{"session_id":"s1","message":"   "}`, http.StatusBadRequest, "message is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, mux, tc.method, "/api/v1/ai/chat", tc.body), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}
}

func TestAIChatStreamsSSECov(t *testing.T) {
	provider := &stubLLMProviderCov{
		available: true,
		deltas:    []string{"Here is ", "a plan."},
		emitDone:  true,
	}
	ai, st, convStore := setupAITestServerCov(t, provider)

	// Seed a pool so the system prompt includes topology context.
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "root", CIDR: "10.0.0.0/8"}); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	conv := newConversationCov(t, convStore, "chat")

	rr := assertStatusCov(t, doReqCov(t, ai.srv.mux, http.MethodPost, "/api/v1/ai/chat",
		`{"session_id":"`+conv+`","message":"Plan a /16 for prod"}`), http.StatusOK)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	body := rr.Body.String()
	for _, want := range []string{`data: {"delta":"Here is "}`, `data: {"delta":"a plan."}`, "event: done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}

	// The service persists the user message and the assembled assistant reply.
	stored, err := convStore.GetConversation(t.Context(), conv)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if len(stored.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2 (user + assistant)", len(stored.Messages))
	}
	if stored.Messages[0].Role != "user" || stored.Messages[0].Content != "Plan a /16 for prod" {
		t.Fatalf("unexpected user message: %+v", stored.Messages[0])
	}
	if stored.Messages[1].Role != "assistant" || stored.Messages[1].Content != "Here is a plan." {
		t.Fatalf("unexpected assistant message: %+v", stored.Messages[1])
	}

	// The system prompt must be the first message sent to the provider and must
	// carry the current topology.
	if len(provider.lastPrompt) == 0 || provider.lastPrompt[0].Role != "system" {
		t.Fatalf("expected a system prompt, got %+v", provider.lastPrompt)
	}
	if !strings.Contains(provider.lastPrompt[0].Content, "10.0.0.0/8") {
		t.Fatal("system prompt should include the existing pool topology")
	}
}

func TestAIChatStreamErrorCov(t *testing.T) {
	provider := &stubLLMProviderCov{available: true, streamErr: errors.New("provider exploded")}
	ai, _, convStore := setupAITestServerCov(t, provider)
	conv := newConversationCov(t, convStore, "chat")

	rr := doReqCov(t, ai.srv.mux, http.MethodPost, "/api/v1/ai/chat",
		`{"session_id":"`+conv+`","message":"hi"}`)
	if rr.Code < 400 {
		t.Fatalf("expected an error status when the provider fails, got %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); strings.Contains(ct, "event-stream") {
		t.Fatalf("failed chat must not open an SSE stream (Content-Type %q)", ct)
	}
}

func TestAIApplyPlanValidationCov(t *testing.T) {
	ai, st, _ := setupAITestServerCov(t, &stubLLMProviderCov{available: true})
	mux := ai.srv.mux
	path := "/api/v1/ai/sessions/s-1/apply-plan"

	tests := []struct {
		name        string
		body        string
		wantErrPart string
	}{
		{"malformed json", `{`, "invalid request body"},
		{"no pools", `{"plan":{"pools":[]}}`, "plan must contain at least one pool"},
		{"missing ref", `{"plan":{"pools":[{"name":"a","cidr":"10.0.0.0/16"}]}}`, "ref is required"},
		{"duplicate ref", `{"plan":{"pools":[{"ref":"r","name":"a","cidr":"10.0.0.0/16"},{"ref":"r","name":"b","cidr":"10.1.0.0/16"}]}}`, "duplicate ref"},
		{"invalid name", `{"plan":{"pools":[{"ref":"r","name":"","cidr":"10.0.0.0/16"}]}}`, "pool 0 (r)"},
		{"invalid cidr", `{"plan":{"pools":[{"ref":"r","name":"a","cidr":"not-a-cidr"}]}}`, "pool 0 (r)"},
		{"prefix too short", `{"plan":{"pools":[{"ref":"r","name":"a","cidr":"10.0.0.0/4"}]}}`, "pool 0 (r)"},
		{"prefix too long", `{"plan":{"pools":[{"ref":"r","name":"a","cidr":"10.0.0.0/31"}]}}`, "pool 0 (r)"},
		{"forward parent ref", `{"plan":{"pools":[{"ref":"child","name":"a","cidr":"10.0.1.0/24","parent_ref":"root"},{"ref":"root","name":"b","cidr":"10.0.0.0/16"}]}}`, "parent_ref"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, tc.body), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); !strings.Contains(e.Error, tc.wantErrPart) {
				t.Fatalf("error = %q, want it to contain %q", e.Error, tc.wantErrPart)
			}
		})
	}

	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("rejected plans must not create pools, got %d", len(pools))
	}
}

func TestAIApplyPlanCreatesHierarchyCov(t *testing.T) {
	ai, st, _ := setupAITestServerCov(t, &stubLLMProviderCov{available: true})

	body := `{"plan":{"pools":[
		{"ref":"root","name":"AI Root","cidr":"10.0.0.0/16","type":"supernet"},
		{"ref":"child","name":"AI Child","cidr":"10.0.1.0/24","type":"subnet","parent_ref":"root"},
		{"ref":"weird","name":"AI Weird","cidr":"10.0.2.0/24","type":"bogus-type"}
	]}}`
	rr := assertStatusCov(t, doReqCov(t, ai.srv.mux, http.MethodPost, "/api/v1/ai/sessions/sess-9/apply-plan", body), http.StatusOK)

	var resp struct {
		Created    int              `json:"created"`
		Skipped    int              `json:"skipped"`
		Errors     []string         `json:"errors"`
		RootPoolID int64            `json:"root_pool_id"`
		PoolMap    map[string]int64 `json:"pool_map"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Created != 3 || resp.Skipped != 0 {
		t.Fatalf("created/skipped = %d/%d, want 3/0", resp.Created, resp.Skipped)
	}
	if len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0], "invalid type") {
		t.Fatalf("expected one invalid-type warning, got %#v", resp.Errors)
	}
	if resp.PoolMap["root"] == 0 || resp.PoolMap["child"] == 0 {
		t.Fatalf("pool_map missing entries: %#v", resp.PoolMap)
	}

	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 3 {
		t.Fatalf("len(pools) = %d, want 3", len(pools))
	}
	byName := map[string]domain.Pool{}
	for _, p := range pools {
		byName[p.Name] = p
	}
	child := byName["AI Child"]
	if child.ParentID == nil || *child.ParentID != resp.PoolMap["root"] {
		t.Fatalf("child parent not wired: %+v", child)
	}
	if byName["AI Weird"].Type != domain.PoolTypeSubnet {
		t.Fatalf("invalid pool type should fall back to subnet, got %q", byName["AI Weird"].Type)
	}
	root := byName["AI Root"]
	if root.Status != domain.PoolStatusPlanned || root.Tags["ai_planner"] != "true" || root.Tags["session_id"] != "sess-9" {
		t.Fatalf("unexpected root pool metadata: %+v", root)
	}
}

// failingPoolStoreCov rejects CreatePool for pools whose name matches failName,
// simulating a storage-layer failure mid-plan.
type failingPoolStoreCov struct {
	*storage.MemoryStore
	failName string
}

func (s *failingPoolStoreCov) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	if in.Name == s.failName {
		return domain.Pool{}, errors.New("simulated storage failure")
	}
	return s.MemoryStore.CreatePool(ctx, in)
}

func TestAIApplyPlanSkipsFailedAndOrphanedPoolsCov(t *testing.T) {
	base := storage.NewMemoryStore()
	st := &failingPoolStoreCov{MemoryStore: base, failName: "Doomed"}
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)
	convStore := storage.NewMemoryConversationStore(base)
	aiSvc := planning.NewAIPlanningService(planning.NewAnalysisService(st), convStore, st, &stubLLMProviderCov{available: true})
	NewAIPlanningServer(srv, aiSvc, convStore).RegisterAIPlanningRoutes()

	body := `{"plan":{"pools":[
		{"ref":"ok","name":"Fresh","cidr":"192.168.0.0/16","type":"supernet"},
		{"ref":"bad","name":"Doomed","cidr":"10.0.0.0/16","type":"supernet"},
		{"ref":"orphan","name":"Orphan","cidr":"10.0.1.0/24","type":"subnet","parent_ref":"bad"}
	]}}`
	rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/ai/sessions/s/apply-plan", body), http.StatusOK)

	var resp struct {
		Created    int              `json:"created"`
		Skipped    int              `json:"skipped"`
		Errors     []string         `json:"errors"`
		RootPoolID int64            `json:"root_pool_id"`
		PoolMap    map[string]int64 `json:"pool_map"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Created != 1 || resp.Skipped != 2 {
		t.Fatalf("created/skipped = %d/%d, want 1/2", resp.Created, resp.Skipped)
	}
	joined := strings.Join(resp.Errors, "|")
	if !strings.Contains(joined, "simulated storage failure") {
		t.Fatalf("expected the storage failure to be reported, got %#v", resp.Errors)
	}
	if !strings.Contains(joined, "not yet created") {
		t.Fatalf("expected the orphaned child to be reported, got %#v", resp.Errors)
	}
	if _, ok := resp.PoolMap["bad"]; ok {
		t.Fatal("failed pool must not appear in pool_map")
	}
	if _, ok := resp.PoolMap["orphan"]; ok {
		t.Fatal("orphaned pool must not appear in pool_map")
	}
	pools, err := base.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0].Name != "Fresh" {
		t.Fatalf("expected only the healthy pool to be created, got %+v", pools)
	}
}

// TestAIChatStreamsThroughMetricsMiddlewareCov exercises the chat handler the
// way a real request reaches it: wrapped by the metrics middleware, which is
// installed by default. The middleware's ResponseWriter wrapper previously
// forwarded only Unwrap, so the handler's http.Flusher assertion failed and
// every AI chat request returned 500 "streaming not supported". The other SSE
// tests call the mux directly and so never saw it.
func TestAIChatStreamsThroughMetricsMiddlewareCov(t *testing.T) {
	provider := &stubLLMProviderCov{available: true, deltas: []string{"Here is ", "a plan."}, emitDone: true}
	ai, _, convStore := setupAITestServerCov(t, provider)
	conv := newConversationCov(t, convStore, "chat")

	metrics := observability.NewMetrics(observability.MetricsConfig{Namespace: "test", Version: "test"})
	handler := observability.MetricsMiddleware(metrics)(ai.srv.mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat",
		strings.NewReader(`{"session_id":"`+conv+`","message":"Plan a /16 for prod"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	body := rr.Body.String()
	for _, want := range []string{`data: {"delta":"Here is "}`, `data: {"delta":"a plan."}`, "event: done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "streaming not supported") {
		t.Fatal("the metrics wrapper hid http.Flusher from the SSE handler")
	}

	// The stream was flushed rather than buffered until the handler returned.
	if !rr.Flushed {
		t.Error("SSE response was never flushed")
	}
}
