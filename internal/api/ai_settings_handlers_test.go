package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/planning"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"

	"gopkg.in/yaml.v3"
)

const ephemeralKeyMessage = "cannot store an api key: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set, so the stored credential would be lost at restart; set the key or use CLOUDPAM_LLM_API_KEY"

type aiSettingsFixture struct {
	mux      *http.ServeMux
	srv      *Server
	settings *storage.MemorySettingsStore
	audit    *audit.MemoryAuditLogger
	manager  *llm.Manager
	key      []byte
	logs     *syncBuffer
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var apiKeyInBody = regexp.MustCompile(`("api_key"\s*:\s*)"[^"]*"`)

// redactAPIKey hides the api_key value of a request body in failure messages.
func redactAPIKey(body string) string {
	return apiKeyInBody.ReplaceAllString(body, `$1"[redacted]"`)
}

func envDefaults() llm.Config {
	return llm.Config{Model: llm.DefaultModel, MaxTokens: llm.DefaultMaxTokens, Temperature: llm.DefaultTemperature}
}

func setupAISettings(t *testing.T, env llm.Config, storageAvailable bool) *aiSettingsFixture {
	t.Helper()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	mux := http.NewServeMux()
	auditLogger := audit.NewMemoryAuditLogger()
	logs := &syncBuffer{}
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: logs})
	srv := NewServer(mux, storage.NewMemoryStore(), logger, nil, auditLogger)
	settings := storage.NewMemorySettingsStore()
	resolver := llm.NewResolver(env, settings, func(c string) (string, error) { return secrets.Decrypt(c, key) })
	manager := llm.NewManager(resolver)
	ss := NewSettingsServer(srv, settings)
	ss.SetAIProvider(manager, resolver, key, storageAvailable)
	ss.RegisterSettingsRoutes()
	return &aiSettingsFixture{mux: mux, srv: srv, settings: settings, audit: auditLogger, manager: manager, key: key, logs: logs}
}

func (f *aiSettingsFixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, req)
	return rr
}

func (f *aiSettingsFixture) getSettings(t *testing.T) domain.AIProviderSettingsResponse {
	t.Helper()
	rr := f.do(t, http.MethodGet, "/api/v1/settings/ai", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET settings/ai = %d: %s", rr.Code, rr.Body.String())
	}
	return decodeAISettings(t, rr)
}

func (f *aiSettingsFixture) patch(t *testing.T, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	rr := f.do(t, http.MethodPatch, "/api/v1/settings/ai", body)
	if rr.Code != want {
		t.Fatalf("PATCH %s = %d, want %d: %s", redactAPIKey(body), rr.Code, want, rr.Body.String())
	}
	return rr
}

func (f *aiSettingsFixture) test(t *testing.T) (domain.AIProviderTestResponse, string) {
	t.Helper()
	rr := f.do(t, http.MethodPost, "/api/v1/settings/ai/test", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("POST settings/ai/test = %d: %s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	var out domain.AIProviderTestResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	return out, raw
}

func (f *aiSettingsFixture) storedDoc(t *testing.T) *domain.AIProviderSettings {
	t.Helper()
	doc, err := f.settings.GetAIProviderSettings(context.Background())
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	return doc
}

func decodeAISettings(t *testing.T, rr *httptest.ResponseRecorder) domain.AIProviderSettingsResponse {
	t.Helper()
	var out domain.AIProviderSettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	return out
}

// fakeOpenAI is an OpenAI-compatible server that records the models it saw.
type fakeOpenAI struct {
	*httptest.Server
	mu     sync.Mutex
	models []string
	status int // non-zero = fail every request with this status and a secret body
}

const leakedBody = `{"error":{"message":"bad key sk-leaked-from-provider"}}`

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.models = append(f.models, req.Model)
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(leakedBody))
			return
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOpenAI) lastModel() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.models) == 0 {
		return ""
	}
	return f.models[len(f.models)-1]
}

func TestAISettingsGetDefaultsWithEnvFallback(t *testing.T) {
	env := envDefaults()
	env.APIKey = "env-secret-key"
	env.Model = "env-model"
	f := setupAISettings(t, env, true)

	rr := f.do(t, http.MethodGet, "/api/v1/settings/ai", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "env-secret-key") {
		t.Fatal("GET response leaks the environment api key")
	}
	got := decodeAISettings(t, rr)
	want := domain.AIProviderSettingSources{
		Endpoint: domain.AISettingSourceDefault, Model: domain.AISettingSourceEnvironment,
		APIKey: domain.AISettingSourceEnvironment, MaxTokens: domain.AISettingSourceDefault,
		Temperature: domain.AISettingSourceDefault,
	}
	if got.Sources != want {
		t.Errorf("sources = %+v, want %+v", got.Sources, want)
	}
	if got.Model != "env-model" || got.MaxTokens != llm.DefaultMaxTokens || got.Temperature != llm.DefaultTemperature || got.Endpoint != "" {
		t.Errorf("effective values = %+v", got)
	}
	if !got.APIKeyConfigured || !got.APIKeyStorageAvailable || got.State != domain.AIProviderStateConfigured {
		t.Errorf("flags/state = %+v", got)
	}
}

func TestAISettingsGetRedactsEnvEndpointCredentials(t *testing.T) {
	env := envDefaults()
	env.Endpoint = "https://user:env-url-secret@llm.example/v1"
	f := setupAISettings(t, env, true)
	rr := f.do(t, http.MethodGet, "/api/v1/settings/ai", "")
	if strings.Contains(rr.Body.String(), "env-url-secret") {
		t.Fatal("GET leaks credentials embedded in the environment endpoint")
	}
	if got := decodeAISettings(t, rr); got.Endpoint != "https://xxxxx@llm.example/v1" || got.State != domain.AIProviderStateConfigured {
		t.Fatalf("GET = %+v", got)
	}
}

func TestAISettingsGetNotConfigured(t *testing.T) {
	got := setupAISettings(t, envDefaults(), true).getSettings(t)
	if got.State != domain.AIProviderStateNotConfigured || got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceNone {
		t.Fatalf("GET = %+v, want not_configured with no key", got)
	}
}

func TestAISettingsPatchStoresEncryptedKey(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	rr := f.patch(t, `{"api_key":"sk-plaintext-123","model":"gpt-4o-mini"}`, http.StatusOK)
	if strings.Contains(rr.Body.String(), "sk-plaintext-123") {
		t.Fatal("PATCH response leaks the api key")
	}
	got := decodeAISettings(t, rr)
	if !got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceDatabase || got.Model != "gpt-4o-mini" || got.Sources.Model != domain.AISettingSourceDatabase {
		t.Fatalf("PATCH response = %+v", got)
	}
	if got.State != domain.AIProviderStateConfigured {
		t.Fatalf("state = %q, want configured", got.State)
	}

	doc := f.storedDoc(t)
	if doc.APIKeyEncrypted == "" || doc.APIKeyEncrypted == "sk-plaintext-123" || strings.Contains(doc.APIKeyEncrypted, "sk-plaintext") {
		t.Fatal("store must hold ciphertext, not the plaintext key")
	}
	if plain, err := secrets.Decrypt(doc.APIKeyEncrypted, f.key); err != nil || plain != "sk-plaintext-123" {
		t.Fatalf("stored ciphertext does not decrypt back (err %v)", err)
	}

	events, _, err := f.audit.List(context.Background(), audit.ListOptions{Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("audit events = %d, err %v; want 1", len(events), err)
	}
	ev := events[0]
	if ev.Action != "update" || ev.ResourceType != "settings" || ev.ResourceID != "ai_provider" {
		t.Fatalf("audit event = %+v", ev)
	}
	evJSON, _ := json.Marshal(ev)
	if strings.Contains(string(evJSON), "sk-plaintext") || strings.Contains(string(evJSON), doc.APIKeyEncrypted) {
		t.Fatal("audit event carries the key or its ciphertext")
	}
	changed, _ := ev.Changes.After["changed"].([]string)
	if strings.Join(changed, ",") != "model,api_key" {
		t.Fatalf("audit changed = %v, want [model api_key]", ev.Changes.After["changed"])
	}
}

func TestAISettingsPatchNoOpSkipsWriteAndAudit(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"http://ollama:11434/v1","model":"llama3","max_tokens":2048,"temperature":0.3}`, http.StatusOK)
	before := f.storedDoc(t)
	countEvents := func() int {
		t.Helper()
		events, _, err := f.audit.List(context.Background(), audit.ListOptions{Limit: 100})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		return len(events)
	}
	eventsBefore := countEvents()
	wantBody := f.do(t, http.MethodGet, "/api/v1/settings/ai", "").Body.String()

	for _, body := range []string{
		`{}`,
		`{"endpoint":"http://ollama:11434/v1","model":"llama3","max_tokens":2048,"temperature":0.3}`,
		`{"endpoint":" http://ollama:11434/v1 ","clear_api_key":true}`,
	} {
		rr := f.patch(t, body, http.StatusOK)
		if got := rr.Body.String(); got != wantBody {
			t.Errorf("PATCH %s body = %s, want the GET representation %s", body, got, wantBody)
		}
		if after := f.storedDoc(t); !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("PATCH %s moved updated_at from %v to %v", body, before.UpdatedAt, after.UpdatedAt)
		}
		if n := countEvents(); n != eventsBefore {
			t.Errorf("PATCH %s recorded %d audit events, want none", body, n-eventsBefore)
		}
	}
}

func TestAISettingsPatchOmittedKeyPreserves(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"api_key":"sk-keep-me"}`, http.StatusOK)
	before := f.storedDoc(t).APIKeyEncrypted
	got := decodeAISettings(t, f.patch(t, `{"model":"other-model"}`, http.StatusOK))
	if after := f.storedDoc(t).APIKeyEncrypted; after != before {
		t.Fatal("omitting api_key changed the stored key")
	}
	if !got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceDatabase {
		t.Fatalf("PATCH response = %+v", got)
	}
}

func TestAISettingsPatchClearAPIKey(t *testing.T) {
	t.Run("falls back to environment", func(t *testing.T) {
		env := envDefaults()
		env.APIKey = "env-key"
		f := setupAISettings(t, env, true)
		f.patch(t, `{"api_key":"sk-db"}`, http.StatusOK)
		got := decodeAISettings(t, f.patch(t, `{"clear_api_key":true}`, http.StatusOK))
		if got.Sources.APIKey != domain.AISettingSourceEnvironment || !got.APIKeyConfigured {
			t.Fatalf("after clear = %+v, want environment key", got)
		}
		if f.storedDoc(t).APIKeyEncrypted != "" {
			t.Fatal("clear_api_key left ciphertext in the store")
		}
	})
	t.Run("none without environment", func(t *testing.T) {
		f := setupAISettings(t, envDefaults(), true)
		f.patch(t, `{"api_key":"sk-db"}`, http.StatusOK)
		got := decodeAISettings(t, f.patch(t, `{"clear_api_key":true}`, http.StatusOK))
		if got.Sources.APIKey != domain.AISettingSourceNone || got.APIKeyConfigured {
			t.Fatalf("after clear = %+v, want no key", got)
		}
	})
}

func decodeAPIError(t *testing.T, rr *httptest.ResponseRecorder) apiError {
	t.Helper()
	var body apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body
}

const endpointNeedsKeyDetail = "re-enter api_key or set clear_api_key when changing the endpoint while a stored key exists"

func TestAISettingsPatchEndpointChangeWithStoredKey(t *testing.T) {
	seed := func(t *testing.T) (*aiSettingsFixture, string) {
		f := setupAISettings(t, envDefaults(), true)
		f.patch(t, `{"endpoint":"https://llm-a.example/v1","api_key":"sk-bound-to-a"}`, http.StatusOK)
		return f, f.storedDoc(t).APIKeyEncrypted
	}

	t.Run("change without key field is rejected", func(t *testing.T) {
		f, before := seed(t)
		for _, body := range []string{`{"endpoint":"https://llm-b.example/v1"}`, `{"endpoint":""}`} {
			rr := f.patch(t, body, http.StatusBadRequest)
			got := decodeAPIError(t, rr)
			if got.Error != "invalid endpoint" || got.Detail != endpointNeedsKeyDetail {
				t.Fatalf("%s: error = %+v", body, got)
			}
			doc := f.storedDoc(t)
			if doc.Endpoint != "https://llm-a.example/v1" || doc.APIKeyEncrypted != before {
				t.Fatalf("%s: rejected PATCH changed the store: endpoint %q", body, doc.Endpoint)
			}
		}
	})
	t.Run("change with a new key stores it", func(t *testing.T) {
		f, before := seed(t)
		got := decodeAISettings(t, f.patch(t, `{"endpoint":"https://llm-b.example/v1","api_key":"sk-bound-to-b"}`, http.StatusOK))
		doc := f.storedDoc(t)
		if doc.Endpoint != "https://llm-b.example/v1" || doc.APIKeyEncrypted == before {
			t.Fatal("endpoint change with api_key did not store a new key")
		}
		if plain, err := secrets.Decrypt(doc.APIKeyEncrypted, f.key); err != nil || plain != "sk-bound-to-b" {
			t.Fatalf("stored key does not decrypt to the new key (err %v)", err)
		}
		if !got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceDatabase {
			t.Fatalf("PATCH response = %+v", got)
		}
	})
	t.Run("change with clear_api_key removes the key", func(t *testing.T) {
		f, _ := seed(t)
		got := decodeAISettings(t, f.patch(t, `{"endpoint":"https://llm-b.example/v1","clear_api_key":true}`, http.StatusOK))
		if f.storedDoc(t).APIKeyEncrypted != "" || got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceNone {
			t.Fatalf("after change + clear = %+v", got)
		}
	})
	t.Run("same endpoint re-sent is not a change", func(t *testing.T) {
		f, before := seed(t)
		f.patch(t, `{"endpoint":"  https://llm-a.example/v1 ","model":"m2"}`, http.StatusOK)
		if doc := f.storedDoc(t); doc.APIKeyEncrypted != before || doc.Model != "m2" {
			t.Fatal("re-sending the same endpoint touched the key or dropped the update")
		}
	})
	t.Run("change without a stored key is allowed", func(t *testing.T) {
		env := envDefaults()
		env.APIKey = "env-key"
		f := setupAISettings(t, env, true)
		f.patch(t, `{"endpoint":"https://llm-a.example/v1"}`, http.StatusOK)
		got := decodeAISettings(t, f.patch(t, `{"endpoint":"https://llm-b.example/v1"}`, http.StatusOK))
		// D1: a database endpoint never receives the environment key.
		if got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceNone || got.Sources.Endpoint != domain.AISettingSourceDatabase {
			t.Fatalf("db endpoint with env key = %+v, want no key", got)
		}
	})
}

func TestAISettingsDatabaseEndpointDoesNotReceiveEnvKey(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(llmSrv.Close)
	env := envDefaults()
	env.APIKey = "sk-env-secret"
	f := setupAISettings(t, env, true)
	f.patch(t, `{"endpoint":"`+llmSrv.URL+`"}`, http.StatusOK)
	f.test(t)
	mu.Lock()
	defer mu.Unlock()
	if len(auths) == 0 {
		t.Fatal("provider was not called")
	}
	for _, a := range auths {
		if a != "" {
			t.Fatal("the environment api key was sent to a database-configured endpoint")
		}
	}
}

func TestAISettingsPatchRejectsLinkLocalEndpoints(t *testing.T) {
	for _, ep := range []string{"http://169.254.169.254/latest", "http://169.254.1.2:11434/v1", "http://[fd00:ec2::254]/v1", "http://[::ffff:169.254.169.254]/v1", "http://[fe80::1]/v1"} {
		f := setupAISettings(t, envDefaults(), true)
		got := decodeAPIError(t, f.patch(t, `{"endpoint":"`+ep+`"}`, http.StatusBadRequest))
		if got.Error != "invalid endpoint" || got.Detail != "link-local and metadata addresses are not allowed" {
			t.Fatalf("%s: error = %+v", ep, got)
		}
	}
	for _, ep := range []string{"http://127.0.0.1:11434/v1", "http://[::1]:8000/v1", "http://10.1.2.3/v1", "http://192.168.1.5:11434/v1", "http://localhost:11434/v1"} {
		f := setupAISettings(t, envDefaults(), true)
		f.patch(t, `{"endpoint":"`+ep+`"}`, http.StatusOK)
	}
}

// TestAISettingsTestResultDoesNotRevertConcurrentPatch lands a PATCH while the
// provider test is in flight; the PATCHed value must survive and the stale
// result must not be stored.
func TestAISettingsTestResultDoesNotRevertConcurrentPatch(t *testing.T) {
	var f *aiSettingsFixture
	var once sync.Once
	patchCode := make(chan int, 1)
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/ai", strings.NewReader(`{"max_tokens":123}`))
			rr := httptest.NewRecorder()
			f.mux.ServeHTTP(rr, req)
			patchCode <- rr.Code
		})
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\n")
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(llmSrv.Close)
	f = setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"`+llmSrv.URL+`"}`, http.StatusOK)

	got, _ := f.test(t)
	if code := <-patchCode; code != http.StatusOK {
		t.Fatalf("concurrent PATCH = %d", code)
	}
	if !got.OK || got.State != domain.AIProviderStateVerified {
		t.Fatalf("test response = %+v, want the live result", got)
	}
	doc := f.storedDoc(t)
	if doc.MaxTokens != 123 {
		t.Fatalf("max_tokens = %d, the test result reverted the concurrent PATCH", doc.MaxTokens)
	}
	if doc.LastTestedAt != nil || doc.State != domain.AIProviderStateConfigured {
		t.Fatalf("stale test result was stored: %+v", doc)
	}
	if !strings.Contains(f.logs.String(), "ai provider settings changed during test; result not stored") {
		t.Fatal("conflict was not logged")
	}
}

func TestAISettingsTestResultNotStoredAfterClientDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Stream {
			cancel() // the client goes away during the streaming check
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(llmSrv.Close)
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"`+llmSrv.URL+`"}`, http.StatusOK)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai/test", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST test = %d: %s", rr.Code, rr.Body.String())
	}
	if doc := f.storedDoc(t); doc.LastTestedAt != nil || doc.State != domain.AIProviderStateConfigured {
		t.Fatalf("result stored after the client disconnected: %+v", doc)
	}
}

func TestRedactAPIKeyHelper(t *testing.T) {
	if got := redactAPIKey(`{"endpoint":"x","api_key" : "sk-hidden"}`); strings.Contains(got, "sk-hidden") {
		t.Fatalf("redactAPIKey = %q", got)
	}
}

func TestAISettingsPatchValidation(t *testing.T) {
	long := strings.Repeat("m", domain.MaxAIProviderModelLength+1)
	cases := map[string]string{
		"bad scheme":            `{"endpoint":"ftp://llm.example/v1"}`,
		"relative endpoint":     `{"endpoint":"/v1"}`,
		"not a url":             `{"endpoint":"llm.example"}`,
		"query":                 `{"endpoint":"http://llm.example/v1?x=1"}`,
		"fragment":              `{"endpoint":"http://llm.example/v1#frag"}`,
		"credentials in url":    `{"endpoint":"http://user:pw@llm.example/v1"}`,
		"model too long":        `{"model":"` + long + `"}`,
		"max tokens negative":   `{"max_tokens":-1}`,
		"max tokens too large":  `{"max_tokens":1000001}`,
		"temperature high":      `{"temperature":2.5}`,
		"temperature low":       `{"temperature":-0.1}`,
		"temperature and clear": `{"temperature":1,"clear_temperature":true}`,
		"empty api key":         `{"api_key":""}`,
		"blank api key":         `{"api_key":"   "}`,
		"key and clear":         `{"api_key":"sk-x","clear_api_key":true}`,
		"invalid json":          `{"model":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := setupAISettings(t, envDefaults(), true)
			rr := f.patch(t, body, http.StatusBadRequest)
			if strings.Contains(rr.Body.String(), "sk-x") {
				t.Fatal("validation error echoes the api key")
			}
			if f.storedDoc(t).HasStoredValues() {
				t.Fatal("rejected PATCH changed the store")
			}
		})
	}
}

func TestAISettingsPatchAcceptsBoundaryValues(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	got := decodeAISettings(t, f.patch(t, `{"endpoint":"https://llm.example/v1","max_tokens":1000000,"temperature":0}`, http.StatusOK))
	if got.Endpoint != "https://llm.example/v1" || got.MaxTokens != 1_000_000 || got.Temperature != 0 || got.Sources.Temperature != domain.AISettingSourceDatabase {
		t.Fatalf("PATCH response = %+v", got)
	}
	// Authless endpoint counts as configured.
	if got.State != domain.AIProviderStateConfigured {
		t.Fatalf("state = %q, want configured for an authless endpoint", got.State)
	}
	got = decodeAISettings(t, f.patch(t, `{"endpoint":"","max_tokens":0,"clear_temperature":true}`, http.StatusOK))
	if got.Sources.Endpoint != domain.AISettingSourceDefault || got.Sources.MaxTokens != domain.AISettingSourceDefault || got.Sources.Temperature != domain.AISettingSourceDefault {
		t.Fatalf("clearing did not reveal defaults: %+v", got.Sources)
	}
}

func TestAISettingsPatchEphemeralKeyConflict(t *testing.T) {
	f := setupAISettings(t, envDefaults(), false)
	if f.getSettings(t).APIKeyStorageAvailable {
		t.Fatal("api_key_storage_available should be false with an ephemeral key")
	}
	rr := f.patch(t, `{"api_key":"sk-ephemeral"}`, http.StatusConflict)
	var body apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != ephemeralKeyMessage {
		t.Fatalf("error = %q, want %q", body.Error, ephemeralKeyMessage)
	}
	if f.storedDoc(t).HasStoredValues() {
		t.Fatal("refused PATCH changed the store")
	}
	// Non-key fields can still be saved.
	f.patch(t, `{"endpoint":"http://ollama:11434/v1"}`, http.StatusOK)
}

func TestAISettingsPatchStateTransitions(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"http://ollama:11434/v1"}`, http.StatusOK)
	tested := time.Now().UTC()
	doc := f.storedDoc(t)
	doc.State = domain.AIProviderStateVerified
	doc.LastTestedAt = &tested
	doc.LastTestModel = "m"
	if err := f.settings.UpdateAIProviderSettings(context.Background(), doc); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := decodeAISettings(t, f.patch(t, `{"max_tokens":100,"temperature":0.5}`, http.StatusOK))
	if got.State != domain.AIProviderStateVerified || got.LastTestedAt == nil {
		t.Fatalf("tuning-only PATCH changed state: %+v", got)
	}
	got = decodeAISettings(t, f.patch(t, `{"model":"new-model"}`, http.StatusOK))
	if got.State != domain.AIProviderStateConfigured || got.LastTestedAt != nil || got.LastTestModel != "" {
		t.Fatalf("material PATCH kept test state: %+v", got)
	}
}

func TestAISettingsTestEndpointSuccess(t *testing.T) {
	llmSrv := newFakeOpenAI(t)
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"`+llmSrv.URL+`","api_key":"sk-test","model":"probe-model"}`, http.StatusOK)

	got, _ := f.test(t)
	if !got.OK || got.State != domain.AIProviderStateVerified || !got.StreamingSupported || got.Error != "" {
		t.Fatalf("test response = %+v", got)
	}
	if got.Model != "probe-model" || got.Endpoint != llmSrv.URL || got.TestedAt.IsZero() || got.LatencyMS < 0 {
		t.Fatalf("test response details = %+v", got)
	}
	doc := f.storedDoc(t)
	if doc.State != domain.AIProviderStateVerified || doc.LastTestedAt == nil || doc.LastTestModel != "probe-model" || doc.LastTestError != "" {
		t.Fatalf("stored test result = %+v", doc)
	}
	if s := f.getSettings(t); s.State != domain.AIProviderStateVerified || s.LastTestModel != "probe-model" {
		t.Fatalf("GET after test = %+v", s)
	}

	events, _, _ := f.audit.List(context.Background(), audit.ListOptions{Action: "test"})
	if len(events) != 1 || events[0].ResourceID != "ai_provider" {
		t.Fatalf("expected one test audit event, got %+v", events)
	}
}

func TestAISettingsTestEndpointHTTP401IsSanitized(t *testing.T) {
	llmSrv := newFakeOpenAI(t)
	llmSrv.status = http.StatusUnauthorized
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"`+llmSrv.URL+`","api_key":"sk-wrong"}`, http.StatusOK)

	got, raw := f.test(t)
	if got.OK || got.State != domain.AIProviderStateFailing || got.Error != "provider returned HTTP 401" || got.StreamingSupported {
		t.Fatalf("test response = %+v", got)
	}
	if strings.Contains(raw, "leaked") || strings.Contains(raw, "sk-wrong") {
		t.Fatal("test response leaks the provider body or key")
	}
	doc := f.storedDoc(t)
	docJSON, _ := json.Marshal(doc)
	if strings.Contains(string(docJSON), "leaked") {
		t.Fatal("stored test result holds the provider body")
	}
	if doc.State != domain.AIProviderStateFailing || doc.LastTestError != "provider returned HTTP 401" {
		t.Fatalf("stored test result = %+v", doc)
	}
}

func TestAISettingsTestEndpointUnreachable(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := closed.URL
	closed.Close()
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"`+url+`"}`, http.StatusOK)

	got, _ := f.test(t)
	if got.OK || got.State != domain.AIProviderStateFailing || got.Error != "endpoint unreachable" {
		t.Fatalf("test response = %+v", got)
	}
}

func TestAISettingsTestEndpointNotConfigured(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	got, _ := f.test(t)
	if got.OK || got.State != domain.AIProviderStateNotConfigured || got.Error != "no provider configured" {
		t.Fatalf("test response = %+v", got)
	}
}

func TestAISettingsPatchAppliesToNextChatAndTestWithoutRestart(t *testing.T) {
	llmSrv := newFakeOpenAI(t)
	f := setupAISettings(t, envDefaults(), true)
	main := storage.NewMemoryStore()
	conv := storage.NewMemoryConversationStore(main)
	aiSvc := planning.NewAIPlanningService(planning.NewAnalysisService(main), conv, main, f.manager)

	if aiSvc.Available(context.Background()) {
		t.Fatal("AI should be unavailable before configuration")
	}
	f.patch(t, `{"endpoint":"`+llmSrv.URL+`","model":"model-one"}`, http.StatusOK)
	if !aiSvc.Available(context.Background()) {
		t.Fatal("AI should be available right after PATCH")
	}
	if _, _ = f.test(t); llmSrv.lastModel() != "model-one" {
		t.Fatalf("test used %q, want model-one", llmSrv.lastModel())
	}

	f.patch(t, `{"model":"model-two"}`, http.StatusOK)
	session, err := aiSvc.CreateConversation(context.Background(), "s")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	ch, err := aiSvc.Chat(context.Background(), session.ID, "plan a /16")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	events := 0
	for range ch {
		events++
	}
	if events == 0 {
		t.Fatal("chat stream produced no events")
	}
	if llmSrv.lastModel() != "model-two" {
		t.Fatalf("chat used %q, want model-two", llmSrv.lastModel())
	}
	if _, _ = f.test(t); llmSrv.lastModel() != "model-two" {
		t.Fatalf("test used %q, want model-two", llmSrv.lastModel())
	}
}

func TestAISettingsUnreadableStoredKeyIsReported(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	if err := f.settings.UpdateAIProviderSettings(context.Background(), &domain.AIProviderSettings{
		Endpoint: "http://ollama:11434/v1", APIKeyEncrypted: "deadbeef",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rr := f.do(t, http.MethodGet, "/api/v1/settings/ai", "")
	if strings.Contains(rr.Body.String(), "deadbeef") {
		t.Fatal("GET leaks the ciphertext")
	}
	got := decodeAISettings(t, rr)
	if got.APIKeyConfigured || got.Sources.APIKey != domain.AISettingSourceNone || got.State != domain.AIProviderStateFailing || got.LastTestError == "" {
		t.Fatalf("GET with unreadable key = %+v", got)
	}
}

func TestProtectedAISettingsRequireSettingsWrite(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	roleStore := auth.NewMemoryRoleStore()
	requireCreateRole(t, roleStore, "settings_read_only", []auth.Permission{
		{Resource: auth.ResourceSettings, Action: auth.ActionRead},
	})
	auth.SetRoleStoreProvider(roleStore)
	t.Cleanup(func() { auth.SetRoleStoreProvider(nil) })

	mux := http.NewServeMux()
	obsLogger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, storage.NewMemoryStore(), obsLogger, nil, nil)
	ss := NewSettingsServer(srv, storage.NewMemorySettingsStore())
	ss.RegisterProtectedSettingsRoutes(func(next http.Handler) http.Handler { return next }, logger)

	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/v1/settings/ai", "", http.StatusOK},
		{http.MethodPatch, "/api/v1/settings/ai", `{"model":"x"}`, http.StatusForbidden},
		{http.MethodPost, "/api/v1/settings/ai/test", "", http.StatusForbidden},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req = req.WithContext(auth.ContextWithRole(req.Context(), auth.Role("settings_read_only")))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Errorf("%s %s = %d, want %d: %s", tc.method, tc.path, rr.Code, tc.want, rr.Body.String())
		}
	}
}

func TestOpenAPIAdvertisesAISettingsRoutes(t *testing.T) {
	mux := http.NewServeMux()
	obsLogger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, storage.NewMemoryStore(), obsLogger, nil, nil)
	srv.registerUnprotectedTestRoutes()
	NewSettingsServer(srv, storage.NewMemorySettingsStore()).RegisterSettingsRoutes()

	raw := srv.openAPISpecYAML()
	var spec map[string]any
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("spec is not valid YAML: %v", err)
	}
	paths := asMap(t, spec["paths"], "paths")
	schemas := asMap(t, asMap(t, spec["components"], "components")["schemas"], "components.schemas")

	refOf := func(holder any, name string) string {
		content := asMap(t, asMap(t, holder, name)["content"], name+".content")
		schema := asMap(t, asMap(t, content["application/json"], name+".json")["schema"], name+".schema")
		ref, _ := schema["$ref"].(string)
		return strings.TrimPrefix(ref, "#/components/schemas/")
	}
	for _, tc := range []struct {
		path, method, request, response string
	}{
		{"/api/v1/settings/ai", "get", "", "AIProviderSettingsResponse"},
		{"/api/v1/settings/ai", "patch", "UpdateAIProviderSettingsRequest", "AIProviderSettingsResponse"},
		{"/api/v1/settings/ai/test", "post", "", "AIProviderTestResponse"},
	} {
		item, ok := paths[tc.path]
		if !ok {
			t.Errorf("spec missing path %s", tc.path)
			continue
		}
		op := asMap(t, asMap(t, item, tc.path)[tc.method], tc.path+"."+tc.method)
		if tc.request != "" {
			if got := refOf(op["requestBody"], tc.path+" request"); got != tc.request {
				t.Errorf("%s %s request schema = %q, want %q", tc.method, tc.path, got, tc.request)
			}
		}
		if got := refOf(asMap(t, op["responses"], "responses")["200"], tc.path+" 200"); got != tc.response {
			t.Errorf("%s %s response schema = %q, want %q", tc.method, tc.path, got, tc.response)
		}
	}
	props := func(name string) map[string]any {
		return asMap(t, asMap(t, schemas[name], name)["properties"], name+".properties")
	}
	for name, fields := range map[string][]string{
		"AIProviderSettingsResponse":      {"api_key_configured", "api_key_storage_available", "sources", "state", "last_test_error"},
		"UpdateAIProviderSettingsRequest": {"endpoint", "model", "max_tokens", "temperature", "clear_temperature", "api_key", "clear_api_key"},
		"AIProviderTestResponse":          {"ok", "state", "latency_ms", "streaming_supported", "error", "tested_at"},
	} {
		p := props(name)
		for _, f := range fields {
			if _, ok := p[f]; !ok {
				t.Errorf("%s schema missing %q", name, f)
			}
		}
	}
	if strings.Contains(string(raw), "api_key_encrypted") {
		t.Error("OpenAPI spec exposes the stored ciphertext field")
	}
}
