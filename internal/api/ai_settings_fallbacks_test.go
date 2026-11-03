package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
)

// fakeAnthropic answers the Messages API with a fixed text reply and records
// the key header it saw.
func fakeAnthropic(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var seenKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey = r.Header.Get("x-api-key")
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(ts.Close)
	return ts, &seenKey
}

func TestAISettingsPatchStoresProviderKindAndFallbacks(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	body := `{"provider":"Anthropic","model":"claude-opus-5-5","api_key":"sk-ant-primary","fallbacks":[` +
		`{"provider":"anthropic","model":"claude-haiku-5-5","api_key":"sk-ant-fallback"},` +
		`{"provider":"openai","endpoint":"http://ollama:11434/v1","model":"llama3"}]}`
	rr := f.patch(t, body, http.StatusOK)
	if s := rr.Body.String(); strings.Contains(s, "sk-ant") {
		t.Fatal("PATCH response leaks a key")
	}

	got := f.getSettings(t)
	if got.Provider != llm.ProviderAnthropic || got.Sources.Provider != domain.AISettingSourceDatabase || got.Model != "claude-opus-5-5" {
		t.Fatalf("primary = %+v", got)
	}
	if len(got.Fallbacks) != 2 {
		t.Fatalf("fallbacks = %+v, want 2", got.Fallbacks)
	}
	first, second := got.Fallbacks[0], got.Fallbacks[1]
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("fallback ids = %q, %q, want distinct server-assigned ids", first.ID, second.ID)
	}
	if first.Provider != llm.ProviderAnthropic || first.Model != "claude-haiku-5-5" || !first.APIKeyConfigured || first.Endpoint != "" {
		t.Fatalf("first fallback = %+v", first)
	}
	if second.Provider != llm.ProviderOpenAI || second.Endpoint != "http://ollama:11434/v1" || second.Model != "llama3" || second.APIKeyConfigured {
		t.Fatalf("second fallback = %+v", second)
	}

	doc := f.storedDoc(t)
	raw, _ := json.Marshal(doc)
	if strings.Contains(string(raw), "sk-ant") {
		t.Fatal("stored document holds a plaintext key")
	}
	if len(doc.Fallbacks) != 2 || doc.Fallbacks[0].APIKeyEncrypted == "" || doc.Fallbacks[1].APIKeyEncrypted != "" {
		t.Fatalf("stored fallbacks = %+v", doc.Fallbacks)
	}

	events, _, _ := f.audit.List(context.Background(), audit.ListOptions{Action: "update"})
	if len(events) != 1 {
		t.Fatalf("audit events = %+v, want one update", events)
	}
	changed, _ := json.Marshal(events[0].Changes.After["changed"])
	for _, want := range []string{"provider", "model", "api_key", "fallbacks"} {
		if !strings.Contains(string(changed), `"`+want+`"`) {
			t.Fatalf("audit changed = %s, want %q", changed, want)
		}
	}
	if strings.Contains(string(changed), "sk-ant") {
		t.Fatal("audit entry holds a key")
	}
}

func TestAISettingsPatchFallbackKeepsKeyByIDAndGuardsMoves(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"http://primary/v1","fallbacks":[{"provider":"anthropic","model":"claude","api_key":"sk-ant-1"},{"provider":"openai","endpoint":"http://ollama:11434/v1"}]}`, http.StatusOK)
	before := f.getSettings(t).Fallbacks
	id1, id2 := before[0].ID, before[1].ID
	ct1 := f.storedDoc(t).Fallbacks[0].APIKeyEncrypted

	// Same entry, model changed, no key sent: the stored key stays.
	f.patch(t, `{"fallbacks":[{"id":"`+id1+`","provider":"anthropic","model":"claude-2"},{"id":"`+id2+`","provider":"openai","endpoint":"http://ollama:11434/v1"}]}`, http.StatusOK)
	doc := f.storedDoc(t)
	if doc.Fallbacks[0].Model != "claude-2" || doc.Fallbacks[0].APIKeyEncrypted != ct1 {
		t.Fatalf("model-only change lost or replaced the key: %+v", doc.Fallbacks[0])
	}

	// Reordering keeps both entries and their keys.
	f.patch(t, `{"fallbacks":[{"id":"`+id2+`","provider":"openai","endpoint":"http://ollama:11434/v1"},{"id":"`+id1+`","provider":"anthropic","model":"claude-2"}]}`, http.StatusOK)
	got := f.getSettings(t).Fallbacks
	if got[0].ID != id2 || got[1].ID != id1 || !got[1].APIKeyConfigured {
		t.Fatalf("reorder = %+v", got)
	}

	// Moving a keyed entry to another endpoint or kind needs the key again.
	for _, body := range []string{
		`{"fallbacks":[{"id":"` + id1 + `","provider":"anthropic","endpoint":"http://gateway","model":"claude-2"}]}`,
		`{"fallbacks":[{"id":"` + id1 + `","provider":"openai","model":"claude-2"}]}`,
	} {
		rr := f.patch(t, body, http.StatusBadRequest)
		if !strings.Contains(rr.Body.String(), "invalid fallbacks") || !strings.Contains(rr.Body.String(), "re-enter api_key") {
			t.Fatalf("move without key = %s", rr.Body.String())
		}
	}
	// With the key re-entered the move is accepted and the ciphertext changes.
	f.patch(t, `{"fallbacks":[{"id":"`+id1+`","provider":"anthropic","endpoint":"http://gateway","model":"claude-2","api_key":"sk-ant-2"}]}`, http.StatusOK)
	doc = f.storedDoc(t)
	if len(doc.Fallbacks) != 1 || doc.Fallbacks[0].Endpoint != "http://gateway" || doc.Fallbacks[0].APIKeyEncrypted == ct1 || doc.Fallbacks[0].APIKeyEncrypted == "" {
		t.Fatalf("move with key = %+v", doc.Fallbacks)
	}

	// Clearing the key of an anthropic entry leaves it unusable, so it is refused.
	rr := f.patch(t, `{"fallbacks":[{"id":"`+id1+`","provider":"anthropic","endpoint":"http://gateway","model":"claude-2","clear_api_key":true}]}`, http.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "anthropic fallback needs api_key") {
		t.Fatalf("clear key on anthropic = %s", rr.Body.String())
	}

	// An unknown id is refused rather than silently treated as new.
	rr = f.patch(t, `{"fallbacks":[{"id":"nope","provider":"openai","endpoint":"http://ollama:11434/v1"}]}`, http.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "unknown id") {
		t.Fatalf("unknown id = %s", rr.Body.String())
	}

	// An empty list clears the fallbacks; omitting the field keeps them.
	f.patch(t, `{"model":"m2"}`, http.StatusOK)
	if got := f.getSettings(t).Fallbacks; len(got) != 1 {
		t.Fatalf("omitted fallbacks must be kept, got %+v", got)
	}
	f.patch(t, `{"fallbacks":[]}`, http.StatusOK)
	if got := f.getSettings(t).Fallbacks; len(got) != 0 {
		t.Fatalf("empty list must clear, got %+v", got)
	}
}

func TestAISettingsPatchFallbackValidation(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	many := strings.Repeat(`{"provider":"openai","endpoint":"http://ollama:11434/v1"},`, domain.MaxAIFallbackProviders+1)
	for _, tc := range []struct {
		name, body, wantErr, wantDetail string
	}{
		{"too many", `{"fallbacks":[` + strings.TrimSuffix(many, ",") + `]}`, "invalid fallbacks", "at most 5"},
		{"bad kind", `{"fallbacks":[{"provider":"cohere","endpoint":"http://x/v1"}]}`, "invalid fallbacks[0].provider", "openai, anthropic"},
		{"missing kind", `{"fallbacks":[{"endpoint":"http://x/v1"}]}`, "invalid fallbacks[0].provider", "openai, anthropic"},
		{"link-local endpoint", `{"fallbacks":[{"provider":"openai","endpoint":"http://169.254.169.254/v1"}]}`, "invalid fallbacks[0].endpoint", "link-local"},
		{"empty key", `{"fallbacks":[{"provider":"openai","api_key":""}]}`, "invalid fallbacks[0].api_key", "must not be empty"},
		{"key and clear", `{"fallbacks":[{"provider":"openai","api_key":"k","clear_api_key":true}]}`, "invalid fallbacks[0].api_key", "not both"},
		{"openai without key or endpoint", `{"fallbacks":[{"provider":"openai","model":"gpt-4o"}]}`, "invalid fallbacks", "needs api_key, or an endpoint"},
		{"anthropic without key", `{"fallbacks":[{"provider":"anthropic","model":"claude"}]}`, "invalid fallbacks", "anthropic fallback needs api_key"},
		{"bad primary kind", `{"provider":"cohere"}`, "invalid provider", "openai, anthropic"},
		{"unknown nested field", `{"fallbacks":[{"provider":"openai","apikey":"k"}]}`, "invalid request body", `unknown field \"apikey\"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := f.patch(t, tc.body, http.StatusBadRequest)
			body := rr.Body.String()
			if !strings.Contains(body, tc.wantErr) || !strings.Contains(body, tc.wantDetail) {
				t.Fatalf("response = %s, want %q and %q", body, tc.wantErr, tc.wantDetail)
			}
		})
	}
	if doc := f.storedDoc(t); doc.HasStoredValues() {
		t.Fatalf("rejected requests must store nothing, got %+v", doc)
	}
}

func TestAISettingsPatchFallbackKeyNeedsDurableStorage(t *testing.T) {
	f := setupAISettings(t, envDefaults(), false)
	rr := f.patch(t, `{"fallbacks":[{"provider":"anthropic","api_key":"sk-ant"}]}`, http.StatusConflict)
	if !strings.Contains(rr.Body.String(), ephemeralKeyMessage) {
		t.Fatalf("response = %s", rr.Body.String())
	}
	// A keyless OpenAI-compatible fallback needs no storage for a key.
	f.patch(t, `{"fallbacks":[{"provider":"openai","endpoint":"http://ollama:11434/v1"}]}`, http.StatusOK)
}

func TestAISettingsPatchProviderKindChangeWithStoredKey(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"api_key":"sk-openai"}`, http.StatusOK)
	f.test(t)
	if s := f.getSettings(t); s.State == domain.AIProviderStateConfigured {
		t.Fatalf("state after test = %q, want a test verdict", s.State)
	}

	rr := f.patch(t, `{"provider":"anthropic"}`, http.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "invalid provider") || !strings.Contains(rr.Body.String(), errEndpointNeedsKey) {
		t.Fatalf("kind change without key = %s", rr.Body.String())
	}
	if s := f.getSettings(t); s.Provider != llm.ProviderOpenAI {
		t.Fatalf("rejected change was stored: %+v", s)
	}

	f.patch(t, `{"provider":"anthropic","api_key":"sk-ant"}`, http.StatusOK)
	s := f.getSettings(t)
	if s.Provider != llm.ProviderAnthropic || s.State != domain.AIProviderStateConfigured || s.LastTestedAt != nil {
		t.Fatalf("kind change must reset the test verdict: %+v", s)
	}
	// Clearing the kind goes back to the environment default; the stored key
	// stays bound to it only when the key is handled in the same save.
	rr = f.patch(t, `{"provider":""}`, http.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "invalid provider") {
		t.Fatalf("clear kind without key = %s", rr.Body.String())
	}
	f.patch(t, `{"provider":"","clear_api_key":true}`, http.StatusOK)
	if s := f.getSettings(t); s.Provider != llm.ProviderOpenAI || s.Sources.Provider != domain.AISettingSourceDefault || s.APIKeyConfigured {
		t.Fatalf("after clearing kind and key = %+v", s)
	}
}

func TestAISettingsDatabaseKindDoesNotReceiveEnvKey(t *testing.T) {
	env := envDefaults()
	env.APIKey = "env-secret-key"
	f := setupAISettings(t, env, true)
	if s := f.getSettings(t); !s.APIKeyConfigured || s.Sources.APIKey != domain.AISettingSourceEnvironment {
		t.Fatalf("baseline = %+v", s)
	}
	f.patch(t, `{"provider":"anthropic"}`, http.StatusOK)
	s := f.getSettings(t)
	if s.APIKeyConfigured || s.Sources.APIKey != domain.AISettingSourceNone {
		t.Fatalf("a stored kind must not redirect CLOUDPAM_LLM_API_KEY to another host: %+v", s)
	}
}

func TestAISettingsTestEndpointReportsEachFallback(t *testing.T) {
	primary := newFakeOpenAI(t)
	anthropicSrv, seenKey := fakeAnthropic(t)
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := closed.URL
	closed.Close()

	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"`+primary.URL+`","model":"probe","fallbacks":[`+
		`{"provider":"anthropic","endpoint":"`+anthropicSrv.URL+`","model":"claude","api_key":"sk-ant-fb"},`+
		`{"provider":"openai","endpoint":"`+deadURL+`/v1","model":"llama3"},`+
		`{"provider":"anthropic","model":"claude","api_key":"sk-ant-x"}]}`, http.StatusOK)
	// The third fallback's key is made unreadable to exercise the unconfigured path.
	doc := f.storedDoc(t)
	doc.Fallbacks[2].APIKeyEncrypted = "garbage"
	if err := f.settings.UpdateAIProviderSettings(context.Background(), doc); err != nil {
		t.Fatal(err)
	}

	got, raw := f.test(t)
	if !got.OK || got.State != domain.AIProviderStateVerified || got.Model != "probe" {
		t.Fatalf("primary result = %+v", got)
	}
	if len(got.Fallbacks) != 3 {
		t.Fatalf("fallbacks = %+v, want 3 results", got.Fallbacks)
	}
	if fb := got.Fallbacks[0]; !fb.OK || fb.Provider != llm.ProviderAnthropic || fb.Model != "claude" || fb.Endpoint != anthropicSrv.URL || fb.Error != "" || fb.LatencyMS < 0 {
		t.Fatalf("anthropic fallback = %+v", fb)
	}
	if *seenKey != "sk-ant-fb" {
		t.Fatalf("anthropic fallback used key %q, want its own stored key", *seenKey)
	}
	if fb := got.Fallbacks[1]; fb.OK || fb.Error != "endpoint unreachable" || fb.Model != "llama3" {
		t.Fatalf("unreachable fallback = %+v", fb)
	}
	if fb := got.Fallbacks[2]; fb.OK || fb.Error != "stored api key cannot be decrypted" {
		t.Fatalf("unreadable-key fallback = %+v", fb)
	}
	if strings.Contains(raw, "sk-ant") {
		t.Fatal("test response leaks a key")
	}
	if s := f.getSettings(t); s.State != domain.AIProviderStateVerified {
		t.Fatalf("a failing fallback must not change the primary's state: %+v", s)
	}
}

func TestAIChatEmitsFallbackEventWhenThePrimaryFails(t *testing.T) {
	primary := &stubLLMProviderCov{available: true, streamErr: &llm.StatusError{Status: 503}}
	secondary := &stubLLMProviderCov{available: true, deltas: []string{"From ", "fallback"}, emitDone: true}
	chain := llm.NewChain([]llm.Link{
		{Provider: primary, Label: "anthropic claude-opus-5-5"},
		{Provider: secondary, Label: "openai gpt-4o"},
	})
	ai, _, convStore := setupAITestServerCov(t, chain)
	conv := newConversationCov(t, convStore, "chat")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := LoggingMiddleware(logger)(AuditMiddleware(&mockAuditLogger{}, logger)(ai.srv.mux))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat",
		strings.NewReader(`{"session_id":"`+conv+`","message":"Plan a /16"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	notice := "event: fallback\ndata: {\"failed\":[{\"error\":\"provider returned HTTP 503\",\"provider\":\"anthropic claude-opus-5-5\"}],\"provider\":\"openai gpt-4o\"}\n\n"
	noticeAt, firstDelta := strings.Index(body, notice), strings.Index(body, `data: {"delta":"From "}`)
	if noticeAt < 0 || firstDelta < 0 || noticeAt > firstDelta {
		t.Fatalf("expected the fallback event before the first delta:\n%s", body)
	}
	if strings.Contains(body, `{"delta":""}`) {
		t.Fatalf("the notice must not be forwarded as an empty delta:\n%s", body)
	}
	if !strings.Contains(body, "event: done") || strings.Contains(body, "event: error") {
		t.Fatalf("a fallback answer ends with done, not error:\n%s", body)
	}
	stored, err := convStore.GetConversation(context.Background(), conv)
	if err != nil {
		t.Fatal(err)
	}
	last := stored.Messages[len(stored.Messages)-1]
	if last.Role != "assistant" || last.Content != "From fallback" {
		t.Fatalf("persisted answer = %+v", last)
	}
}
