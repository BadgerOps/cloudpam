package storage

import (
	"context"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

func TestMemoryAIProviderSettingsDefaultsWhenMissing(t *testing.T) {
	got, err := NewMemorySettingsStore().GetAIProviderSettings(context.Background())
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.HasStoredValues() || got.State != "" {
		t.Fatalf("fresh store = %+v, want nothing stored", got)
	}
}

func TestMemoryAIProviderSettingsCloneIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySettingsStore()
	temp := 0.3
	tested := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	callerTested := tested
	in := &domain.AIProviderSettings{
		Model:           "gpt-4o-mini",
		Temperature:     &temp,
		APIKeyEncrypted: "cafe",
		State:           domain.AIProviderStateVerified,
		LastTestedAt:    &callerTested,
	}
	if err := store.UpdateAIProviderSettings(ctx, in); err != nil {
		t.Fatalf("UpdateAIProviderSettings: %v", err)
	}

	// Mutating the caller's input after the write must not reach the store.
	temp = 1.9
	*in.LastTestedAt = tested.Add(time.Hour)
	in.Model = "changed"

	got, err := store.GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if got.Model != "gpt-4o-mini" || *got.Temperature != 0.3 || !got.LastTestedAt.Equal(tested) {
		t.Fatalf("store changed through the write argument: %+v", got)
	}

	// Mutating a read result must not reach the store either.
	*got.Temperature = 1.5
	*got.LastTestedAt = tested.Add(2 * time.Hour)
	got.APIKeyEncrypted = ""

	again, err := store.GetAIProviderSettings(ctx)
	if err != nil {
		t.Fatalf("GetAIProviderSettings: %v", err)
	}
	if *again.Temperature != 0.3 || !again.LastTestedAt.Equal(tested) || again.APIKeyEncrypted != "cafe" {
		t.Fatalf("store changed through a read result: %+v", again)
	}
}
