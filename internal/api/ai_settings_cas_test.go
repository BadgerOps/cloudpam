package api

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// sideWritingSettingsStore writes a different field to the document the first
// time it is read, as a concurrent PATCH or test result landing between the
// handler's read and its write would.
type sideWritingSettingsStore struct {
	*storage.MemorySettingsStore
	once sync.Once
}

func (s *sideWritingSettingsStore) GetAIProviderSettings(ctx context.Context) (*domain.AIProviderSettings, error) {
	doc, err := s.MemorySettingsStore.GetAIProviderSettings(ctx)
	if err != nil {
		return nil, err
	}
	s.once.Do(func() {
		other := doc.Clone()
		other.MaxTokens = 777
		other.UpdatedAt = other.UpdatedAt.Add(time.Second)
		_ = s.UpdateAIProviderSettings(ctx, other)
	})
	return doc, nil
}

// alwaysConflictingSettingsStore reports a conflict on every conditional write.
type alwaysConflictingSettingsStore struct {
	*storage.MemorySettingsStore
	attempts atomic.Int32
}

func (s *alwaysConflictingSettingsStore) UpdateAIProviderSettingsIfUnchanged(context.Context, time.Time, *domain.AIProviderSettings) error {
	s.attempts.Add(1)
	return storage.ErrConflict
}

func TestAISettingsPatchRetriesAfterConcurrentWrite(t *testing.T) {
	mem := storage.NewMemorySettingsStore()
	f := setupAISettingsWithStore(t, envDefaults(), true, mem, &sideWritingSettingsStore{MemorySettingsStore: mem})

	got := decodeAISettings(t, f.patch(t, `{"model":"cas-model"}`, http.StatusOK))
	if got.Model != "cas-model" || got.MaxTokens != 777 {
		t.Fatalf("response = %+v, want both the PATCH and the concurrent write", got)
	}
	doc := f.storedDoc(t)
	if doc.Model != "cas-model" || doc.MaxTokens != 777 {
		t.Fatalf("stored doc = %+v, want model from the PATCH and max_tokens from the concurrent write", doc)
	}
}

func TestAISettingsPatchConflictsAfterRepeatedConcurrentWrites(t *testing.T) {
	mem := storage.NewMemorySettingsStore()
	store := &alwaysConflictingSettingsStore{MemorySettingsStore: mem}
	f := setupAISettingsWithStore(t, envDefaults(), true, mem, store)

	rr := f.patch(t, `{"model":"cas-model"}`, http.StatusConflict)
	if e := decodeAPIError(t, rr); e.Error != "ai provider settings changed concurrently" || e.Detail != "retry the request" {
		t.Fatalf("error = %+v", e)
	}
	if n := store.attempts.Load(); n != 3 {
		t.Fatalf("conditional writes = %d, want 3 attempts", n)
	}
	if doc := f.storedDoc(t); doc.Model != "" {
		t.Fatalf("stored doc changed despite the conflict: %+v", doc)
	}
	if events, _, _ := f.audit.List(context.Background(), audit.ListOptions{Action: "update"}); len(events) != 0 {
		t.Fatalf("audit event written for a failed PATCH: %+v", events)
	}
}

func TestAISettingsTestResultAdvancesCASToken(t *testing.T) {
	llmSrv := newFakeOpenAI(t)
	env := envDefaults()
	env.Endpoint = llmSrv.URL
	f := setupAISettings(t, env, true)

	before := f.storedDoc(t).UpdatedAt
	if got, _ := f.test(t); !got.OK {
		t.Fatalf("test = %+v", got)
	}
	after := f.storedDoc(t).UpdatedAt
	if !after.After(before) {
		t.Fatalf("UpdatedAt after test = %v, want after %v", after, before)
	}
}
