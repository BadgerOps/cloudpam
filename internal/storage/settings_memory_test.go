package storage

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
)

func TestMemorySettingsStoreGetReturnsIsolatedCopy(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySettingsStore()

	got, err := store.GetSecuritySettings(ctx)
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}

	// Mutate every nested reference type the caller can reach.
	got.TrustedProxies = append(got.TrustedProxies, "10.0.0.0/8")
	got.APIKeyAllowedScopesByRole["admin"] = []string{"*"}
	got.APIKeyAllowedScopesByRole["intruder"] = []string{"*"}
	got.SessionDurationHours = 999

	after, err := store.GetSecuritySettings(ctx)
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}

	if len(after.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want empty", after.TrustedProxies)
	}
	if _, ok := after.APIKeyAllowedScopesByRole["intruder"]; ok {
		t.Error("caller mutation added a role to store-owned scope policy")
	}
	if got, want := len(after.APIKeyAllowedScopesByRole["admin"]), len(domain.DefaultAPIKeyAllowedScopesByRole()["admin"]); got != want {
		t.Errorf("admin scopes = %d entries, want %d", got, want)
	}
	if after.SessionDurationHours != domain.DefaultSecuritySettings().SessionDurationHours {
		t.Errorf("SessionDurationHours = %d, want default", after.SessionDurationHours)
	}
}

func TestMemorySettingsStoreUpdateCopiesCallerState(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySettingsStore()

	input := domain.DefaultSecuritySettings()
	input.TrustedProxies = []string{"192.168.0.0/16"}
	input.APIKeyAllowedScopesByRole = map[string][]string{"viewer": {"pools:read"}}

	if err := store.UpdateSecuritySettings(ctx, &input); err != nil {
		t.Fatalf("UpdateSecuritySettings: %v", err)
	}

	// Mutating the caller's struct after the write must not reach the store.
	input.TrustedProxies[0] = "0.0.0.0/0"
	input.APIKeyAllowedScopesByRole["viewer"] = []string{"*"}
	input.APIKeyAllowedScopesByRole["admin"] = []string{"*"}

	after, err := store.GetSecuritySettings(ctx)
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	if len(after.TrustedProxies) != 1 || after.TrustedProxies[0] != "192.168.0.0/16" {
		t.Errorf("TrustedProxies = %v, want [192.168.0.0/16]", after.TrustedProxies)
	}
	if scopes := after.APIKeyAllowedScopesByRole["viewer"]; len(scopes) != 1 || scopes[0] != "pools:read" {
		t.Errorf("viewer scopes = %v, want [pools:read]", scopes)
	}
}

func TestMemorySettingsStoreDefaultsAreNotShared(t *testing.T) {
	ctx := context.Background()
	a := NewMemorySettingsStore()
	b := NewMemorySettingsStore()

	settings, err := a.GetSecuritySettings(ctx)
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	settings.APIKeyAllowedScopesByRole["viewer"] = []string{"*"}
	if err := a.UpdateSecuritySettings(ctx, settings); err != nil {
		t.Fatalf("UpdateSecuritySettings: %v", err)
	}

	other, err := b.GetSecuritySettings(ctx)
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	if scopes := other.APIKeyAllowedScopesByRole["viewer"]; len(scopes) == 1 && scopes[0] == "*" {
		t.Error("second store shares the default scope policy map with the first")
	}
}
