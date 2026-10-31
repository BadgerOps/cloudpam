package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// TestAISettingsTestResultBoundToTestedConfiguration restarts the server with
// a different CLOUDPAM_LLM_ENDPOINT over the same store: the stored verified
// result must not vouch for a provider that was never tested.
func TestAISettingsTestResultBoundToTestedConfiguration(t *testing.T) {
	llmSrv := newFakeOpenAI(t)
	mem := storage.NewMemorySettingsStore()
	env := envDefaults()
	env.Endpoint = llmSrv.URL
	f := setupAISettingsWithStore(t, env, true, mem, mem)

	if got, _ := f.test(t); !got.OK {
		t.Fatalf("test = %+v", got)
	}
	before := f.getSettings(t)
	if before.State != domain.AIProviderStateVerified || before.LastTestedAt == nil {
		t.Fatalf("GET after test = %+v", before)
	}
	if doc := f.storedDoc(t); doc.LastTestFingerprint == "" {
		t.Fatal("test result stored without a fingerprint")
	}

	other := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(other.Close)
	env.Endpoint = other.URL
	restarted := setupAISettingsWithStore(t, env, true, mem, mem)

	after := restarted.getSettings(t)
	if after.State != domain.AIProviderStateConfigured {
		t.Fatalf("state after endpoint change = %q, want configured", after.State)
	}
	if after.LastTestedAt == nil || !after.LastTestedAt.Equal(*before.LastTestedAt) || after.LastTestModel != before.LastTestModel {
		t.Fatalf("test metadata not kept: %+v", after)
	}
	if after.LastTestError != "" {
		t.Fatalf("last_test_error reported for an untested configuration: %q", after.LastTestError)
	}

	// Testing the new configuration binds the result to it again.
	if got, _ := restarted.test(t); got.OK || got.State != domain.AIProviderStateFailing {
		t.Fatalf("test against 404 server = %+v", got)
	}
	if s := restarted.getSettings(t); s.State != domain.AIProviderStateFailing || s.LastTestError == "" {
		t.Fatalf("GET after retest = %+v", s)
	}
	// The original server sees the failing result as stale for its own endpoint.
	if s := f.getSettings(t); s.State != domain.AIProviderStateConfigured || s.LastTestError != "" {
		t.Fatalf("original configuration reports the other configuration's result: %+v", s)
	}
}

func TestAISettingsLegacyTestResultWithoutFingerprintIsKept(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)
	f.patch(t, `{"endpoint":"http://ollama:11434/v1"}`, http.StatusOK)
	doc := f.storedDoc(t)
	doc.State = domain.AIProviderStateFailing
	doc.LastTestError = "provider returned HTTP 500"
	doc.LastTestFingerprint = ""
	if err := f.settings.UpdateAIProviderSettings(t.Context(), doc); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if s := f.getSettings(t); s.State != domain.AIProviderStateFailing || s.LastTestError != "provider returned HTTP 500" {
		t.Fatalf("document written before fingerprints lost its state: %+v", s)
	}
}
