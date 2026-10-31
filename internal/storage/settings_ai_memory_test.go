package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

func TestMemoryAIProviderSettingsCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySettingsStore()
	t1 := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	t2 := t1.Add(time.Second)

	// A missing document matches only the zero time.
	if err := store.UpdateAIProviderSettingsIfUnchanged(ctx, t1, &domain.AIProviderSettings{Model: "x", UpdatedAt: t1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-zero expected on missing document: err = %v, want ErrConflict", err)
	}
	if err := store.UpdateAIProviderSettingsIfUnchanged(ctx, time.Time{}, &domain.AIProviderSettings{Model: "a", UpdatedAt: t1}); err != nil {
		t.Fatalf("zero expected on missing document: %v", err)
	}
	if err := store.UpdateAIProviderSettingsIfUnchanged(ctx, time.Time{}, &domain.AIProviderSettings{Model: "zero"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("zero expected on stored document: err = %v, want ErrConflict", err)
	}
	// Matching updated_at (compared as an instant) succeeds.
	if err := store.UpdateAIProviderSettingsIfUnchanged(ctx, t1.In(time.FixedZone("x", 3600)), &domain.AIProviderSettings{Model: "b", UpdatedAt: t2}); err != nil {
		t.Fatalf("matching expected: %v", err)
	}
	// The stored updated_at is now t2, so t1 no longer matches.
	if err := store.UpdateAIProviderSettingsIfUnchanged(ctx, t1, &domain.AIProviderSettings{Model: "stale", UpdatedAt: t1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale expected: err = %v, want ErrConflict", err)
	}
	got, _ := store.GetAIProviderSettings(ctx)
	if got.Model != "b" || !got.UpdatedAt.Equal(t2) {
		t.Fatalf("stored = %+v, want model b at t2", got)
	}
}

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
