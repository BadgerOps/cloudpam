package domain

import (
	"reflect"
	"testing"
)

func TestDefaultSecuritySettingsValues(t *testing.T) {
	s := DefaultSecuritySettings()

	checks := []struct {
		field string
		got   int
		want  int
	}{
		{"SessionDurationHours", s.SessionDurationHours, 24},
		{"MaxSessionsPerUser", s.MaxSessionsPerUser, 10},
		{"PasswordMinLength", s.PasswordMinLength, 12},
		{"PasswordMaxLength", s.PasswordMaxLength, 72},
		{"LoginRateLimitPerMin", s.LoginRateLimitPerMin, 5},
		{"AccountLockoutAttempts", s.AccountLockoutAttempts, 0},
		{"AccountLockoutCooldownMinutes", s.AccountLockoutCooldownMinutes, 15},
		{"APIKeyDefaultExpiryDays", s.APIKeyDefaultExpiryDays, 0},
		{"APIKeyMaxLifetimeDays", s.APIKeyMaxLifetimeDays, 0},
		{"APIKeyRotationReminderDays", s.APIKeyRotationReminderDays, 14},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.field, c.got, c.want)
		}
	}

	if !s.LocalAuthEnabled {
		t.Error("LocalAuthEnabled should default to true")
	}
	if s.TrustedProxies == nil {
		t.Error("TrustedProxies should default to a non-nil empty slice")
	}
	if len(s.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want empty", s.TrustedProxies)
	}
	if s.APIKeyAllowedScopesByRole == nil {
		t.Fatal("APIKeyAllowedScopesByRole should default to the role scope map")
	}
}

func TestDefaultSecuritySettingsReturnsIndependentMaps(t *testing.T) {
	first := DefaultSecuritySettings()
	first.APIKeyAllowedScopesByRole["viewer"] = []string{"hacked"}
	first.TrustedProxies = append(first.TrustedProxies, "10.0.0.0/8")

	second := DefaultSecuritySettings()
	if got := second.APIKeyAllowedScopesByRole["viewer"]; reflect.DeepEqual(got, []string{"hacked"}) {
		t.Error("mutating one DefaultSecuritySettings result leaked into the next")
	}
	if len(second.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies leaked: %v", second.TrustedProxies)
	}
}

func TestDefaultAPIKeyAllowedScopesByRoleEnvelopes(t *testing.T) {
	scopes := DefaultAPIKeyAllowedScopesByRole()

	roles := []string{"admin", "operator", "viewer", "auditor"}
	if len(scopes) != len(roles) {
		t.Errorf("expected %d roles, got %d (%v)", len(roles), len(scopes), scopes)
	}

	has := func(role, scope string) bool {
		for _, s := range scopes[role] {
			if s == scope {
				return true
			}
		}
		return false
	}

	allowed := []struct{ role, scope string }{
		{"admin", "*"},
		{"admin", "audit:read"},
		{"admin", "keys:write"},
		{"operator", "pools:write"},
		{"operator", "accounts:write"},
		{"operator", "discovery:write"},
		{"viewer", "pools:read"},
		{"viewer", "discovery:read"},
		{"auditor", "audit:read"},
	}
	for _, c := range allowed {
		if !has(c.role, c.scope) {
			t.Errorf("role %q should be allowed to issue scope %q", c.role, c.scope)
		}
	}

	// Scope-elevation guards: non-admin roles must not be able to issue
	// scopes beyond their own privileges.
	denied := []struct{ role, scope string }{
		{"operator", "*"},
		{"operator", "keys:write"},
		{"operator", "keys:read"},
		{"operator", "audit:read"},
		{"viewer", "*"},
		{"viewer", "pools:write"},
		{"viewer", "accounts:write"},
		{"viewer", "audit:read"},
		{"auditor", "*"},
		{"auditor", "pools:read"},
		{"auditor", "accounts:read"},
	}
	for _, c := range denied {
		if has(c.role, c.scope) {
			t.Errorf("role %q must not be allowed to issue scope %q", c.role, c.scope)
		}
	}
}

func TestDefaultAPIKeyAllowedScopesByRoleReturnsFreshMap(t *testing.T) {
	first := DefaultAPIKeyAllowedScopesByRole()
	first["auditor"] = append(first["auditor"], "pools:write")
	delete(first, "viewer")

	second := DefaultAPIKeyAllowedScopesByRole()
	if _, ok := second["viewer"]; !ok {
		t.Error("deleting a role from one result removed it from the next")
	}
	for _, s := range second["auditor"] {
		if s == "pools:write" {
			t.Error("appending to one result leaked into the next")
		}
	}
}

func TestNormalizeSecuritySettingsNilReturnsDefaults(t *testing.T) {
	got := NormalizeSecuritySettings(nil)
	if got == nil {
		t.Fatal("NormalizeSecuritySettings(nil) returned nil")
	}
	want := DefaultSecuritySettings()
	if got.SessionDurationHours != want.SessionDurationHours ||
		got.PasswordMinLength != want.PasswordMinLength ||
		got.AccountLockoutCooldownMinutes != want.AccountLockoutCooldownMinutes {
		t.Errorf("NormalizeSecuritySettings(nil) = %+v, want defaults %+v", *got, want)
	}
	if got.APIKeyAllowedScopesByRole == nil {
		t.Error("defaults should include the API key scope map")
	}
}

func TestNormalizeSecuritySettingsFillsMissingFields(t *testing.T) {
	settings := &SecuritySettings{
		SessionDurationHours:          8,
		AccountLockoutCooldownMinutes: 0,
		APIKeyRotationReminderDays:    -3,
	}

	got := NormalizeSecuritySettings(settings)
	if got != settings {
		t.Fatal("NormalizeSecuritySettings should normalize the supplied pointer in place")
	}
	if got.SessionDurationHours != 8 {
		t.Errorf("explicit SessionDurationHours was overwritten: %d", got.SessionDurationHours)
	}
	if got.AccountLockoutCooldownMinutes != DefaultSecuritySettings().AccountLockoutCooldownMinutes {
		t.Errorf("AccountLockoutCooldownMinutes = %d, want default", got.AccountLockoutCooldownMinutes)
	}
	if got.APIKeyRotationReminderDays != DefaultSecuritySettings().APIKeyRotationReminderDays {
		t.Errorf("negative APIKeyRotationReminderDays = %d, want default", got.APIKeyRotationReminderDays)
	}
	if got.TrustedProxies == nil {
		t.Error("nil TrustedProxies should be normalized to an empty slice")
	}
	if !reflect.DeepEqual(got.APIKeyAllowedScopesByRole, DefaultAPIKeyAllowedScopesByRole()) {
		t.Errorf("nil scope map should be filled with defaults, got %v", got.APIKeyAllowedScopesByRole)
	}
}

func TestNormalizeSecuritySettingsPreservesExplicitValues(t *testing.T) {
	settings := &SecuritySettings{
		AccountLockoutCooldownMinutes: 42,
		APIKeyRotationReminderDays:    0,
		TrustedProxies:                []string{"10.0.0.0/8"},
		APIKeyAllowedScopesByRole: map[string][]string{
			"operator": {"pools:read"},
		},
	}

	got := NormalizeSecuritySettings(settings)
	if got.AccountLockoutCooldownMinutes != 42 {
		t.Errorf("AccountLockoutCooldownMinutes = %d, want 42", got.AccountLockoutCooldownMinutes)
	}
	if got.APIKeyRotationReminderDays != 0 {
		t.Errorf("zero APIKeyRotationReminderDays is valid (disabled), got %d", got.APIKeyRotationReminderDays)
	}
	if !reflect.DeepEqual(got.TrustedProxies, []string{"10.0.0.0/8"}) {
		t.Errorf("TrustedProxies = %v, want unchanged", got.TrustedProxies)
	}
	// The caller-supplied operator entry wins; missing roles are backfilled.
	if !reflect.DeepEqual(got.APIKeyAllowedScopesByRole["operator"], []string{"pools:read"}) {
		t.Errorf("operator scopes = %v, want the caller-supplied value", got.APIKeyAllowedScopesByRole["operator"])
	}
	for _, role := range []string{"admin", "viewer", "auditor"} {
		if _, ok := got.APIKeyAllowedScopesByRole[role]; !ok {
			t.Errorf("role %q should be backfilled from defaults", role)
		}
	}
	if !reflect.DeepEqual(got.APIKeyAllowedScopesByRole["viewer"], DefaultAPIKeyAllowedScopesByRole()["viewer"]) {
		t.Errorf("backfilled viewer scopes = %v, want defaults", got.APIKeyAllowedScopesByRole["viewer"])
	}
}

func TestNormalizeSecuritySettingsBackfillDoesNotAliasDefaults(t *testing.T) {
	settings := &SecuritySettings{APIKeyAllowedScopesByRole: map[string][]string{"admin": {"*"}}}

	got := NormalizeSecuritySettings(settings)
	viewer := got.APIKeyAllowedScopesByRole["viewer"]
	if len(viewer) == 0 {
		t.Fatal("viewer scopes should be backfilled")
	}
	viewer[0] = "tampered"

	fresh := DefaultAPIKeyAllowedScopesByRole()
	if fresh["viewer"][0] == "tampered" {
		t.Error("backfilled scopes alias the package default slice")
	}
}

func TestNormalizeSecuritySettingsReplacesNilScopeSlices(t *testing.T) {
	settings := &SecuritySettings{
		APIKeyAllowedScopesByRole: map[string][]string{
			"admin":    nil,
			"operator": {},
			"custom":   nil,
		},
	}

	got := NormalizeSecuritySettings(settings)
	for role, scopes := range got.APIKeyAllowedScopesByRole {
		if scopes == nil {
			t.Errorf("role %q kept a nil scope slice", role)
		}
	}
	if len(got.APIKeyAllowedScopesByRole["admin"]) != 0 {
		t.Errorf("explicit nil admin scopes should become empty, got %v", got.APIKeyAllowedScopesByRole["admin"])
	}
	if _, ok := got.APIKeyAllowedScopesByRole["custom"]; !ok {
		t.Error("unknown roles should be preserved")
	}
}
