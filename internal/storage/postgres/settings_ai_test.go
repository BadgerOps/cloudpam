//go:build postgres

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/secrets"
)

func TestAIProviderSettingsPostgresRoundTrip(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	got, err := s.GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings (missing): %v", err)
	}
	if got.HasStoredValues() || got.State != "" {
		t.Fatalf("missing row = %+v, want nothing stored", got)
	}

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ct, err := secrets.Encrypt("sk-pg-plaintext", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	temp := 1.1
	tested := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	in := &domain.AIProviderSettings{
		Model:           "gpt-4o-mini",
		Temperature:     &temp,
		APIKeyEncrypted: ct,
		State:           domain.AIProviderStateVerified,
		LastTestedAt:    &tested,
	}
	if err := s.UpdateAIProviderSettings(ctx, in); err != nil {
		t.Fatalf("UpdateAIProviderSettings: %v", err)
	}

	var raw string
	if err := testDB.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'ai_provider'`).Scan(&raw); err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	if strings.Contains(raw, "sk-pg-plaintext") {
		t.Fatal("stored document holds the plaintext api key")
	}

	got, err = reopenStore(t).GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.Model != "gpt-4o-mini" || got.Temperature == nil || *got.Temperature != 1.1 || got.State != domain.AIProviderStateVerified {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if plain, err := secrets.Decrypt(got.APIKeyEncrypted, key); err != nil || plain != "sk-pg-plaintext" {
		t.Fatalf("stored key does not decrypt back: err=%v", err)
	}
}
