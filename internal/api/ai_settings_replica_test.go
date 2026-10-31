package api

import (
	"context"
	"net/http"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"
)

// TestAISettingsTwoReplicasShareOneStore runs two settings servers, each with
// its own llm.Manager, over one shared settings store (as two replicas over
// one database). Every change made on one replica must be visible to the
// other on its next request, without a restart or any delay.
func TestAISettingsTwoReplicasShareOneStore(t *testing.T) {
	llmSrv := newFakeOpenAI(t)
	shared := storage.NewMemorySettingsStore()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	a := newAISettingsFixture(t, envDefaults(), true, key, shared, shared)
	b := newAISettingsFixture(t, envDefaults(), true, key, shared, shared)
	if a.manager == b.manager {
		t.Fatal("replicas must not share a manager")
	}

	// PATCH on A → GET on B sees the stored values.
	a.patch(t, `{"endpoint":"`+llmSrv.URL+`","model":"replica-model"}`, http.StatusOK)
	onB := b.getSettings(t)
	if onB.Endpoint != llmSrv.URL || onB.Model != "replica-model" {
		t.Fatalf("GET on B after PATCH on A = %+v", onB)
	}
	if onB.Sources.Endpoint != domain.AISettingSourceDatabase || onB.Sources.Model != domain.AISettingSourceDatabase {
		t.Fatalf("sources on B = %+v, want database for endpoint and model", onB.Sources)
	}
	if onB.State != domain.AIProviderStateConfigured {
		t.Fatalf("state on B = %q, want configured", onB.State)
	}

	// B's manager resolves the configuration A stored.
	provider, resolved, err := b.manager.Provider(context.Background())
	if err != nil {
		t.Fatalf("B Provider: %v", err)
	}
	if resolved.Config.Endpoint != llmSrv.URL || resolved.Config.Model != "replica-model" || !provider.Available() {
		t.Fatalf("B resolved %+v (available=%v)", resolved.Config, provider.Available())
	}

	// Test on A → GET on B reports verified for the same configuration.
	if got, _ := a.test(t); !got.OK || got.State != domain.AIProviderStateVerified {
		t.Fatalf("test on A = %+v", got)
	}
	if llmSrv.lastModel() != "replica-model" {
		t.Fatalf("test used model %q", llmSrv.lastModel())
	}
	onB = b.getSettings(t)
	if onB.State != domain.AIProviderStateVerified || onB.LastTestedAt == nil || onB.LastTestModel != "replica-model" {
		t.Fatalf("GET on B after test on A = %+v", onB)
	}

	// PATCH model on B → GET on A drops back to configured.
	b.patch(t, `{"model":"replica-model-2"}`, http.StatusOK)
	onA := a.getSettings(t)
	if onA.Model != "replica-model-2" || onA.State != domain.AIProviderStateConfigured || onA.LastTestedAt != nil {
		t.Fatalf("GET on A after PATCH on B = %+v", onA)
	}

	// A's next provider resolution picks up B's change too.
	if _, resolved, err := a.manager.Provider(context.Background()); err != nil || resolved.Config.Model != "replica-model-2" {
		t.Fatalf("A resolved model %q (err %v), want replica-model-2", resolved.Config.Model, err)
	}
}
