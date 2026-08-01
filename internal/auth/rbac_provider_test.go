package auth

import (
	"context"
	"errors"
	"testing"
)

// rpFakeRoleStore is a RoleStore stub that lets tests drive the
// package-level role provider without depending on MemoryRoleStore.
type rpFakeRoleStore struct {
	roles map[Role]*RoleDefinition
	err   error
}

func (s *rpFakeRoleStore) ListPermissions(context.Context) ([]PermissionDefinition, error) {
	return PermissionCatalog(), nil
}

func (s *rpFakeRoleStore) ListRoles(context.Context) ([]*RoleDefinition, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]*RoleDefinition, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	return out, nil
}

func (s *rpFakeRoleStore) GetRole(_ context.Context, name Role) (*RoleDefinition, error) {
	if s.err != nil {
		return nil, s.err
	}
	role, ok := s.roles[name]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return role, nil
}

func (s *rpFakeRoleStore) CreateRole(context.Context, *RoleDefinition) error { return nil }

func (s *rpFakeRoleStore) UpdateRole(context.Context, Role, string, []Permission) (*RoleDefinition, error) {
	return nil, nil
}

func (s *rpFakeRoleStore) DeleteRole(context.Context, Role) error { return nil }

func (s *rpFakeRoleStore) RoleAssignedToActiveUsers(context.Context, Role) (bool, error) {
	return false, nil
}

// rpUseProvider installs a role provider for the duration of a test and
// restores the previous (default nil) provider afterwards.
func rpUseProvider(t *testing.T, provider RoleStore) {
	t.Helper()
	previous := currentRoleProvider()
	SetRoleStoreProvider(provider)
	t.Cleanup(func() { SetRoleStoreProvider(previous) })
}

func TestRoleProviderIsAuthoritativeOverStaticPermissions(t *testing.T) {
	rpUseProvider(t, &rpFakeRoleStore{roles: map[Role]*RoleDefinition{
		RoleAdmin: {
			ID:          string(RoleAdmin),
			Name:        RoleAdmin,
			IsBuiltin:   true,
			Permissions: []Permission{{ResourcePools, ActionRead}},
		},
	}})

	if !HasPermission(RoleAdmin, ResourcePools, ActionRead) {
		t.Error("provider-granted pools:read should be allowed")
	}
	if HasPermission(RoleAdmin, ResourcePools, ActionCreate) {
		t.Error("provider must be authoritative: admin was narrowed to pools:read")
	}
	if HasPermission(RoleAdmin, ResourceAudit, ActionRead) {
		t.Error("provider must be authoritative: admin was narrowed to pools:read")
	}

	perms := GetPermissions(RoleAdmin)
	if len(perms) != 1 || perms[0].String() != "pools:read" {
		t.Errorf("GetPermissions(admin) = %v, want [pools:read]", perms)
	}
}

func TestRoleProviderGrantsCustomRoles(t *testing.T) {
	rpUseProvider(t, &rpFakeRoleStore{roles: map[Role]*RoleDefinition{
		Role("netops"): {
			ID:   "netops",
			Name: Role("netops"),
			Permissions: []Permission{
				{ResourcePools, ActionRead},
				{ResourcePools, ActionList},
			},
		},
	}})

	tests := []struct {
		name     string
		role     Role
		resource string
		action   string
		want     bool
	}{
		{"custom role granted read", Role("netops"), ResourcePools, ActionRead, true},
		{"custom role granted list", Role("netops"), ResourcePools, ActionList, true},
		{"custom role denied create", Role("netops"), ResourcePools, ActionCreate, false},
		{"custom role denied other resources", Role("netops"), ResourceAccounts, ActionRead, false},
		{"unknown role still denied", Role("ghost"), ResourcePools, ActionRead, false},
		{"no role always denied", RoleNone, ResourcePools, ActionRead, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPermission(tt.role, tt.resource, tt.action); got != tt.want {
				t.Errorf("HasPermission(%q, %q, %q) = %v, want %v", tt.role, tt.resource, tt.action, got, tt.want)
			}
		})
	}

	// Built-in roles the provider does not know about fall back to the
	// static permission cache.
	if !HasPermission(RoleViewer, ResourcePools, ActionRead) {
		t.Error("viewer should fall back to static permissions when the provider has no entry")
	}
	if HasPermission(RoleViewer, ResourcePools, ActionDelete) {
		t.Error("static fallback must not grant viewer pools:delete")
	}
}

func TestRoleProviderErrorFallsBackToStaticPermissions(t *testing.T) {
	rpUseProvider(t, &rpFakeRoleStore{err: errors.New("role backend unavailable")})

	if !HasPermission(RoleAdmin, ResourceAudit, ActionRead) {
		t.Error("a failing provider should fall back to static admin permissions")
	}
	if HasPermission(RoleViewer, ResourcePools, ActionDelete) {
		t.Error("static fallback must still deny viewer pools:delete")
	}
	if HasPermission(Role("netops"), ResourcePools, ActionRead) {
		t.Error("unknown role must be denied when the provider fails")
	}

	perms := GetPermissions(RoleAuditor)
	if len(perms) != len(GetStaticPermissions(RoleAuditor)) {
		t.Errorf("GetPermissions(auditor) = %v, want the static auditor set", perms)
	}
}

func TestSetRoleStoreProviderIsReversible(t *testing.T) {
	if currentRoleProvider() != nil {
		t.Fatal("expected no role provider to be configured by default")
	}

	provider := &rpFakeRoleStore{roles: map[Role]*RoleDefinition{
		RoleAdmin: {Name: RoleAdmin, Permissions: nil},
	}}
	rpUseProvider(t, provider)

	if currentRoleProvider() != provider {
		t.Fatal("SetRoleStoreProvider did not install the provider")
	}
	if HasPermission(RoleAdmin, ResourcePools, ActionRead) {
		t.Error("provider granting no permissions should deny everything for admin")
	}

	SetRoleStoreProvider(nil)
	if currentRoleProvider() != nil {
		t.Fatal("SetRoleStoreProvider(nil) did not clear the provider")
	}
	if !HasPermission(RoleAdmin, ResourcePools, ActionRead) {
		t.Error("clearing the provider should restore static admin permissions")
	}
}

func TestRoleExistsUsesProvider(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRoleStore()
	if err := store.CreateRole(ctx, &RoleDefinition{
		Name:        Role("netops"),
		Permissions: []Permission{{ResourcePools, ActionRead}},
	}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	rpUseProvider(t, store)

	if !RoleExists(ctx, Role("netops")) {
		t.Error("custom role registered with the provider should exist")
	}
	if RoleExists(ctx, Role("ghost")) {
		t.Error("unregistered role should not exist")
	}
	for _, role := range ValidRoles() {
		if !RoleExists(ctx, role) {
			t.Errorf("built-in role %q should always exist", role)
		}
	}
}

func TestGetRolePermissionsContextViaProviderIsIsolated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRoleStore()
	if err := store.CreateRole(ctx, &RoleDefinition{
		Name:        Role("netops"),
		Permissions: []Permission{{ResourcePools, ActionRead}},
	}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	rpUseProvider(t, store)

	perms := GetRolePermissionsContext(ctx, Role("netops"))
	if len(perms) != 1 || perms[0].String() != "pools:read" {
		t.Fatalf("got %v, want [pools:read]", perms)
	}

	perms[0] = Permission{Resource: ResourceUsers, Action: ActionDelete}
	again := GetRolePermissionsContext(ctx, Role("netops"))
	if len(again) != 1 || again[0].String() != "pools:read" {
		t.Errorf("mutating returned permissions corrupted the role store: %v", again)
	}
	if HasPermission(Role("netops"), ResourceUsers, ActionDelete) {
		t.Error("role escalated to users:delete through a returned-slice mutation")
	}
}

func TestScopeAllowedByRolePermissionsUsesProvider(t *testing.T) {
	ctx := context.Background()
	rpUseProvider(t, &rpFakeRoleStore{roles: map[Role]*RoleDefinition{
		Role("netops"): {
			Name: Role("netops"),
			Permissions: []Permission{
				{ResourcePools, ActionRead},
				{ResourcePools, ActionList},
			},
		},
	}})

	if !ScopeAllowedByRolePermissions(ctx, Role("netops"), "pools:read") {
		t.Error("netops should be able to issue pools:read")
	}
	if ScopeAllowedByRolePermissions(ctx, Role("netops"), "pools:write") {
		t.Error("netops must not be able to issue pools:write")
	}
	if ScopeAllowedByRolePermissions(ctx, Role("netops"), "*") {
		t.Error("netops must not be able to issue the full wildcard")
	}
}
