//go:build sqlite

package sqlite

import (
	"errors"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"
)

func TestAIProviderSettingsCompareAndSwap(t *testing.T) {
	s := newTestStore(t)
	ctx := timeoutCtx(t)
	t1 := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	t2 := t1.Add(time.Second)

	// A missing row matches only the zero time.
	if err := s.UpdateAIProviderSettingsIfUnchanged(ctx, t1, &domain.AIProviderSettings{Model: "x", UpdatedAt: t1}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("non-zero expected on missing row: err = %v, want ErrConflict", err)
	}
	if err := s.UpdateAIProviderSettingsIfUnchanged(ctx, time.Time{}, &domain.AIProviderSettings{Model: "a", UpdatedAt: t1}); err != nil {
		t.Fatalf("zero expected on missing row: %v", err)
	}
	if err := s.UpdateAIProviderSettingsIfUnchanged(ctx, time.Time{}, &domain.AIProviderSettings{Model: "zero"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("zero expected on stored row: err = %v, want ErrConflict", err)
	}
	if err := s.UpdateAIProviderSettingsIfUnchanged(ctx, t1.In(time.FixedZone("x", 3600)), &domain.AIProviderSettings{Model: "b", UpdatedAt: t2}); err != nil {
		t.Fatalf("matching expected: %v", err)
	}
	if err := s.UpdateAIProviderSettingsIfUnchanged(ctx, t1, &domain.AIProviderSettings{Model: "stale", UpdatedAt: t1}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale expected: err = %v, want ErrConflict", err)
	}
	got, err := s.GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.Model != "b" || !got.UpdatedAt.Equal(t2) {
		t.Fatalf("stored = %+v, want model b at t2", got)
	}
}

func TestAIProviderSettingsDefaultsWhenMissing(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetAIProviderSettings(timeoutCtx(t))
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.HasStoredValues() || got.State != "" {
		t.Fatalf("missing row = %+v, want nothing stored", got)
	}
}

func TestAIProviderSettingsRoundTripEncryptedKey(t *testing.T) {
	s := newTestStore(t)
	ctx := timeoutCtx(t)
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ct, err := secrets.Encrypt("sk-sqlite-plaintext", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	temp := 0.2
	tested := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	in := &domain.AIProviderSettings{
		Endpoint:        "http://ollama:11434/v1",
		Model:           "llama3",
		MaxTokens:       2048,
		Temperature:     &temp,
		APIKeyEncrypted: ct,
		State:           domain.AIProviderStateFailing,
		LastTestedAt:    &tested,
		LastTestError:   "provider returned HTTP 401",
		LastTestModel:   "llama3",
	}
	if err := s.UpdateAIProviderSettings(ctx, in); err != nil {
		t.Fatalf("UpdateAIProviderSettings: %v", err)
	}

	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'ai_provider'`).Scan(&raw); err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	if raw == "" || strings.Contains(raw, "sk-sqlite-plaintext") {
		t.Fatal("stored document holds the plaintext api key")
	}

	got, err := s.GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.Endpoint != in.Endpoint || got.Model != in.Model || got.MaxTokens != 2048 || got.Temperature == nil || *got.Temperature != 0.2 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.State != domain.AIProviderStateFailing || got.LastTestedAt == nil || !got.LastTestedAt.Equal(tested) || got.LastTestError != in.LastTestError {
		t.Fatalf("test fields mismatch: %+v", got)
	}
	plain, err := secrets.Decrypt(got.APIKeyEncrypted, key)
	if err != nil || plain != "sk-sqlite-plaintext" {
		t.Fatalf("stored key does not decrypt back: err=%v", err)
	}
}

func TestAIProviderSettingsNormalizesPartialDocument(t *testing.T) {
	s := newTestStore(t)
	ctx := timeoutCtx(t)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ('ai_provider', ?, datetime('now'))`,
		`{"model":"  m  ","max_tokens":99999999,"state":"weird"}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := s.GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.Model != "m" || got.MaxTokens != domain.MaxAIProviderMaxTokens || got.State != domain.AIProviderStateConfigured {
		t.Fatalf("partial document not normalized: %+v", got)
	}
}
