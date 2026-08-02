package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// rsTestUserStore is a minimal UserStore used to drive
// RoleAssignedToActiveUsers without depending on MemoryUserStore internals.
type rsTestUserStore struct {
	users []*User
	err   error
}

func (s *rsTestUserStore) Create(context.Context, *User) error { return nil }
func (s *rsTestUserStore) GetByID(context.Context, string) (*User, error) {
	return nil, nil
}
func (s *rsTestUserStore) GetByUsername(context.Context, string) (*User, error) {
	return nil, nil
}
func (s *rsTestUserStore) List(context.Context) ([]*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.users, nil
}
func (s *rsTestUserStore) Update(context.Context, *User) error  { return nil }
func (s *rsTestUserStore) Delete(context.Context, string) error { return nil }
func (s *rsTestUserStore) UpdateLastLogin(context.Context, string, time.Time) error {
	return nil
}
func (s *rsTestUserStore) GetByOIDCIdentity(context.Context, string, string) (*User, error) {
	return nil, nil
}

func TestNormalizeRoleNameLowercasesAndTrims(t *testing.T) {
	tests := []struct {
		in   string
		want Role
	}{
		{"admin", RoleAdmin},
		{"  Admin  ", RoleAdmin},
		{"NETOPS", Role("netops")},
		{"\tnet_ops \n", Role("net_ops")},
		{"", RoleNone},
		{"   ", RoleNone},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := NormalizeRoleName(tt.in); got != tt.want {
				t.Errorf("NormalizeRoleName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsBuiltinRoleCoversEveryValidRole(t *testing.T) {
	for _, role := range ValidRoles() {
		if !IsBuiltinRole(role) {
			t.Errorf("ValidRoles() member %q should be built-in", role)
		}
	}
	for _, role := range []Role{RoleNone, "netops", "Admin", "superuser"} {
		if IsBuiltinRole(role) {
			t.Errorf("IsBuiltinRole(%q) = true, want false", role)
		}
	}
}

func TestValidateCustomRoleNameRules(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		wantErr error
	}{
		{"builtin admin rejected", RoleAdmin, ErrBuiltinRole},
		{"builtin operator rejected", RoleOperator, ErrBuiltinRole},
		{"builtin viewer rejected", RoleViewer, ErrBuiltinRole},
		{"builtin auditor rejected", RoleAuditor, ErrBuiltinRole},
		{"empty rejected", RoleNone, ErrInvalidRole},
		{"single character rejected", Role("a"), ErrInvalidRole},
		{"uppercase rejected", Role("NetOps"), ErrInvalidRole},
		{"leading digit rejected", Role("1netops"), ErrInvalidRole},
		{"leading underscore rejected", Role("_netops"), ErrInvalidRole},
		{"space rejected", Role("net ops"), ErrInvalidRole},
		{"colon rejected", Role("net:ops"), ErrInvalidRole},
		{"too long rejected", Role("a" + strings.Repeat("b", 63)), ErrInvalidRole},
		{"two characters accepted", Role("no"), nil},
		{"underscores and dashes accepted", Role("net_ops-2"), nil},
		{"max length accepted", Role("a" + strings.Repeat("b", 62)), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCustomRoleName(tt.role)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateCustomRoleName(%q) = %v, want %v", tt.role, err, tt.wantErr)
			}
		})
	}
}

func TestBuiltinRoleDefinitionMatchesStaticPermissions(t *testing.T) {
	for _, role := range ValidRoles() {
		t.Run(string(role), func(t *testing.T) {
			def := BuiltinRoleDefinition(role)
			if def == nil {
				t.Fatalf("BuiltinRoleDefinition(%q) = nil", role)
			}
			if def.ID != string(role) || def.Name != role {
				t.Errorf("ID/Name = %q/%q, want %q", def.ID, def.Name, role)
			}
			if !def.IsBuiltin {
				t.Error("IsBuiltin should be true")
			}
			if def.Description == "" {
				t.Error("built-in role should carry a description")
			}
			want := GetStaticPermissions(role)
			if len(def.Permissions) != len(want) {
				t.Fatalf("permission count = %d, want %d", len(def.Permissions), len(want))
			}
			for i := range want {
				if def.Permissions[i] != want[i] {
					t.Errorf("permission[%d] = %v, want %v", i, def.Permissions[i], want[i])
				}
			}
		})
	}
}

func TestBuiltinRoleDefinitionUnknownRoleIsNil(t *testing.T) {
	for _, role := range []Role{RoleNone, "netops", "ADMIN"} {
		if def := BuiltinRoleDefinition(role); def != nil {
			t.Errorf("BuiltinRoleDefinition(%q) = %+v, want nil", role, def)
		}
	}
}

func TestMemoryRoleStoreSeedsBuiltinRoles(t *testing.T) {
	store := NewMemoryRoleStore()
	ctx := context.Background()

	roles, err := store.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != len(ValidRoles()) {
		t.Fatalf("got %d roles, want %d", len(roles), len(ValidRoles()))
	}

	// Built-ins come first, then alphabetically by name.
	wantOrder := []Role{RoleAdmin, RoleAuditor, RoleOperator, RoleViewer}
	for i, want := range wantOrder {
		if roles[i].Name != want {
			t.Errorf("roles[%d].Name = %q, want %q", i, roles[i].Name, want)
		}
		if !roles[i].IsBuiltin {
			t.Errorf("roles[%d] (%q) should be built-in", i, roles[i].Name)
		}
	}
}

func TestMemoryRoleStoreListPermissionsMatchesCatalog(t *testing.T) {
	store := NewMemoryRoleStore()
	perms, err := store.ListPermissions(context.Background())
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	catalog := PermissionCatalog()
	if len(perms) != len(catalog) {
		t.Fatalf("got %d permissions, want %d", len(perms), len(catalog))
	}
	for i := range catalog {
		if perms[i] != catalog[i] {
			t.Errorf("permission[%d] = %+v, want %+v", i, perms[i], catalog[i])
		}
	}
}

func TestMemoryRoleStoreGetRoleNormalizesAndReports404(t *testing.T) {
	store := NewMemoryRoleStore()
	ctx := context.Background()

	def, err := store.GetRole(ctx, Role("  ADMIN "))
	if err != nil {
		t.Fatalf("GetRole(ADMIN): %v", err)
	}
	if def.Name != RoleAdmin {
		t.Errorf("Name = %q, want %q", def.Name, RoleAdmin)
	}

	if _, err := store.GetRole(ctx, Role("netops")); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("GetRole(netops) err = %v, want ErrRoleNotFound", err)
	}
}

func TestMemoryRoleStoreGetRoleReturnsIsolatedCopy(t *testing.T) {
	store := NewMemoryRoleStore()
	ctx := context.Background()

	first, err := store.GetRole(ctx, RoleViewer)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	originalCount := len(first.Permissions)
	first.Description = "tampered"
	first.Permissions[0] = Permission{Resource: ResourceUsers, Action: ActionDelete}
	first.Permissions = append(first.Permissions, Permission{Resource: ResourceSettings, Action: ActionWrite})

	second, err := store.GetRole(ctx, RoleViewer)
	if err != nil {
		t.Fatalf("GetRole (second): %v", err)
	}
	if second.Description == "tampered" {
		t.Error("mutating the returned definition corrupted the store description")
	}
	if len(second.Permissions) != originalCount {
		t.Errorf("permission count = %d, want %d", len(second.Permissions), originalCount)
	}
	for _, p := range second.Permissions {
		if p.Resource == ResourceUsers || p.Resource == ResourceSettings {
			t.Errorf("viewer gained escalated permission %v from a returned-copy mutation", p)
		}
	}
}

func TestMemoryRoleStoreListRolesReturnsIsolatedCopies(t *testing.T) {
	store := NewMemoryRoleStore()
	ctx := context.Background()

	roles, err := store.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	roles[0].Permissions = nil
	roles[0].Description = "tampered"

	again, err := store.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles (second): %v", err)
	}
	if again[0].Description == "tampered" || len(again[0].Permissions) == 0 {
		t.Errorf("mutating a listed role corrupted the store: %+v", again[0])
	}
}

func TestMemoryRoleStoreCreateRole(t *testing.T) {
	ctx := context.Background()

	t.Run("nil role rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.CreateRole(ctx, nil); !errors.Is(err, ErrInvalidRole) {
			t.Errorf("err = %v, want ErrInvalidRole", err)
		}
	})

	t.Run("builtin name rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		err := store.CreateRole(ctx, &RoleDefinition{Name: Role("Admin")})
		if !errors.Is(err, ErrBuiltinRole) {
			t.Errorf("err = %v, want ErrBuiltinRole", err)
		}
	})

	t.Run("invalid name rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		err := store.CreateRole(ctx, &RoleDefinition{Name: Role("bad name")})
		if !errors.Is(err, ErrInvalidRole) {
			t.Errorf("err = %v, want ErrInvalidRole", err)
		}
	})

	t.Run("invalid permission rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		err := store.CreateRole(ctx, &RoleDefinition{
			Name:        Role("netops"),
			Permissions: []Permission{{Resource: ResourcePools, Action: "explode"}},
		})
		if !errors.Is(err, ErrInvalidPermission) {
			t.Errorf("err = %v, want ErrInvalidPermission", err)
		}
		if _, err := store.GetRole(ctx, Role("netops")); !errors.Is(err, ErrRoleNotFound) {
			t.Error("role must not be persisted when permissions are invalid")
		}
	})

	t.Run("success normalizes and stamps metadata", func(t *testing.T) {
		store := NewMemoryRoleStore()
		role := &RoleDefinition{
			Name:        Role("  NetOps  "),
			Description: "network operators",
			IsBuiltin:   true, // must be forced to false
			Permissions: []Permission{{Resource: ResourcePools, Action: ActionRead}},
		}
		before := time.Now().UTC().Add(-time.Second)
		if err := store.CreateRole(ctx, role); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}

		stored, err := store.GetRole(ctx, Role("netops"))
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		if stored.ID != "netops" || stored.Name != Role("netops") {
			t.Errorf("ID/Name = %q/%q, want netops", stored.ID, stored.Name)
		}
		if stored.IsBuiltin {
			t.Error("custom roles must never be flagged built-in")
		}
		if stored.CreatedAt.Before(before) || stored.UpdatedAt.Before(before) {
			t.Errorf("timestamps not stamped: created=%v updated=%v", stored.CreatedAt, stored.UpdatedAt)
		}
		if len(stored.Permissions) != 1 || stored.Permissions[0].String() != "pools:read" {
			t.Errorf("permissions = %v, want [pools:read]", stored.Permissions)
		}
	})

	t.Run("duplicate rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.CreateRole(ctx, &RoleDefinition{Name: Role("netops")}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		err := store.CreateRole(ctx, &RoleDefinition{Name: Role("NETOPS")})
		if !errors.Is(err, ErrRoleExists) {
			t.Errorf("err = %v, want ErrRoleExists", err)
		}
	})
}

func TestMemoryRoleStoreCreateRoleDoesNotAliasCallerPermissions(t *testing.T) {
	store := NewMemoryRoleStore()
	ctx := context.Background()

	perms := []Permission{{Resource: ResourcePools, Action: ActionRead}}
	role := &RoleDefinition{Name: Role("netops"), Permissions: perms}
	if err := store.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}

	// Mutating the caller's slice after the write must not reach the store.
	perms[0] = Permission{Resource: ResourceUsers, Action: ActionDelete}
	role.Description = "tampered"

	stored, err := store.GetRole(ctx, Role("netops"))
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if stored.Permissions[0].String() != "pools:read" {
		t.Errorf("stored permission = %q, want pools:read", stored.Permissions[0].String())
	}
	if stored.Description == "tampered" {
		t.Error("mutating the caller's definition after Create corrupted the store")
	}
}

func TestMemoryRoleStoreUpdateRole(t *testing.T) {
	ctx := context.Background()

	t.Run("builtin rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		_, err := store.UpdateRole(ctx, Role("ADMIN"), "x", nil)
		if !errors.Is(err, ErrBuiltinRole) {
			t.Errorf("err = %v, want ErrBuiltinRole", err)
		}
	})

	t.Run("invalid permission rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.CreateRole(ctx, &RoleDefinition{Name: Role("netops")}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		_, err := store.UpdateRole(ctx, Role("netops"), "x", []Permission{{Resource: "secrets", Action: ActionRead}})
		if !errors.Is(err, ErrInvalidPermission) {
			t.Errorf("err = %v, want ErrInvalidPermission", err)
		}
	})

	t.Run("missing role rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		_, err := store.UpdateRole(ctx, Role("ghost"), "x", nil)
		if !errors.Is(err, ErrRoleNotFound) {
			t.Errorf("err = %v, want ErrRoleNotFound", err)
		}
	})

	t.Run("success replaces description and permissions", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.CreateRole(ctx, &RoleDefinition{
			Name:        Role("netops"),
			Description: "old",
			Permissions: []Permission{{Resource: ResourcePools, Action: ActionRead}},
		}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		created, err := store.GetRole(ctx, Role("netops"))
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}

		newPerms := []Permission{
			{Resource: ResourceAccounts, Action: ActionRead},
			{Resource: ResourceAccounts, Action: ActionList},
		}
		updated, err := store.UpdateRole(ctx, Role(" NetOps "), "  new description  ", newPerms)
		if err != nil {
			t.Fatalf("UpdateRole: %v", err)
		}
		if updated.Description != "new description" {
			t.Errorf("Description = %q, want trimmed %q", updated.Description, "new description")
		}
		if len(updated.Permissions) != 2 {
			t.Fatalf("permissions = %v, want 2 entries", updated.Permissions)
		}
		if updated.CreatedAt != created.CreatedAt {
			t.Errorf("CreatedAt changed: %v -> %v", created.CreatedAt, updated.CreatedAt)
		}
		if updated.UpdatedAt.Before(created.UpdatedAt) {
			t.Errorf("UpdatedAt went backwards: %v -> %v", created.UpdatedAt, updated.UpdatedAt)
		}

		// Old permission is gone.
		reread, err := store.GetRole(ctx, Role("netops"))
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		for _, p := range reread.Permissions {
			if p.Resource == ResourcePools {
				t.Errorf("stale permission %v survived the update", p)
			}
		}

		// Mutating the caller's permission slice must not reach the store.
		newPerms[0] = Permission{Resource: ResourceUsers, Action: ActionDelete}
		reread, err = store.GetRole(ctx, Role("netops"))
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		if reread.Permissions[0].Resource != ResourceAccounts {
			t.Errorf("store aliases the caller's permission slice: %v", reread.Permissions[0])
		}
	})
}

func TestMemoryRoleStoreDeleteRole(t *testing.T) {
	ctx := context.Background()

	t.Run("builtin rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.DeleteRole(ctx, RoleAdmin); !errors.Is(err, ErrBuiltinRole) {
			t.Errorf("err = %v, want ErrBuiltinRole", err)
		}
		if _, err := store.GetRole(ctx, RoleAdmin); err != nil {
			t.Errorf("built-in role should still exist: %v", err)
		}
	})

	t.Run("missing rejected", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.DeleteRole(ctx, Role("ghost")); !errors.Is(err, ErrRoleNotFound) {
			t.Errorf("err = %v, want ErrRoleNotFound", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		store := NewMemoryRoleStore()
		if err := store.CreateRole(ctx, &RoleDefinition{Name: Role("netops")}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		if err := store.DeleteRole(ctx, Role(" NETOPS ")); err != nil {
			t.Fatalf("DeleteRole: %v", err)
		}
		if _, err := store.GetRole(ctx, Role("netops")); !errors.Is(err, ErrRoleNotFound) {
			t.Errorf("role still present after delete: %v", err)
		}
	})

	t.Run("in use by active user rejected", func(t *testing.T) {
		users := &rsTestUserStore{users: []*User{
			{ID: "u1", Username: "a", Role: Role("netops"), IsActive: true},
		}}
		store := NewMemoryRoleStore(users)
		if err := store.CreateRole(ctx, &RoleDefinition{Name: Role("netops")}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		if err := store.DeleteRole(ctx, Role("netops")); !errors.Is(err, ErrRoleInUse) {
			t.Errorf("err = %v, want ErrRoleInUse", err)
		}
		if _, err := store.GetRole(ctx, Role("netops")); err != nil {
			t.Errorf("role should survive a rejected delete: %v", err)
		}
	})

	t.Run("inactive holder does not block delete", func(t *testing.T) {
		users := &rsTestUserStore{users: []*User{
			{ID: "u1", Username: "a", Role: Role("netops"), IsActive: false},
			{ID: "u2", Username: "b", Role: RoleViewer, IsActive: true},
		}}
		store := NewMemoryRoleStore(users)
		if err := store.CreateRole(ctx, &RoleDefinition{Name: Role("netops")}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		if err := store.DeleteRole(ctx, Role("netops")); err != nil {
			t.Errorf("DeleteRole: %v", err)
		}
	})

	t.Run("user store error is propagated", func(t *testing.T) {
		boom := errors.New("user store unavailable")
		store := NewMemoryRoleStore(&rsTestUserStore{err: boom})
		if err := store.CreateRole(ctx, &RoleDefinition{Name: Role("netops")}); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		if err := store.DeleteRole(ctx, Role("netops")); !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})
}

func TestMemoryRoleStoreRoleAssignedToActiveUsers(t *testing.T) {
	ctx := context.Background()

	t.Run("no user store means never in use", func(t *testing.T) {
		store := NewMemoryRoleStore()
		inUse, err := store.RoleAssignedToActiveUsers(ctx, Role("netops"))
		if err != nil {
			t.Fatalf("RoleAssignedToActiveUsers: %v", err)
		}
		if inUse {
			t.Error("expected false when no user store is wired up")
		}
	})

	t.Run("matches only active holders", func(t *testing.T) {
		users := &rsTestUserStore{users: []*User{
			{ID: "u1", Role: Role("netops"), IsActive: false},
			{ID: "u2", Role: RoleViewer, IsActive: true},
		}}
		store := NewMemoryRoleStore(users)

		inUse, err := store.RoleAssignedToActiveUsers(ctx, Role("netops"))
		if err != nil {
			t.Fatalf("RoleAssignedToActiveUsers: %v", err)
		}
		if inUse {
			t.Error("inactive holder should not count as in use")
		}

		inUse, err = store.RoleAssignedToActiveUsers(ctx, RoleViewer)
		if err != nil {
			t.Fatalf("RoleAssignedToActiveUsers: %v", err)
		}
		if !inUse {
			t.Error("active holder should count as in use")
		}
	})
}
