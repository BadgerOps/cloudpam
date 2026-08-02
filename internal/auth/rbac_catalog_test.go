package auth

import (
	"context"
	"testing"
	"time"
)

func TestPermissionCatalogIsWellFormed(t *testing.T) {
	catalog := PermissionCatalog()
	if len(catalog) == 0 {
		t.Fatal("permission catalog is empty")
	}

	seen := make(map[string]bool, len(catalog))
	for _, def := range catalog {
		if seen[def.ID] {
			t.Errorf("duplicate permission ID %q", def.ID)
		}
		seen[def.ID] = true

		want := Permission{Resource: def.Resource, Action: def.Action}.String()
		if def.ID != want {
			t.Errorf("ID %q does not match resource:action %q", def.ID, want)
		}
		if def.Name == "" {
			t.Errorf("permission %q has no name", def.ID)
		}
		if def.Description == "" {
			t.Errorf("permission %q has no description", def.ID)
		}
		if def.Category == "" {
			t.Errorf("permission %q has no category", def.ID)
		}
		if !IsValidPermission(Permission{Resource: def.Resource, Action: def.Action}) {
			t.Errorf("catalog entry %q is rejected by IsValidPermission", def.ID)
		}
	}

	for _, id := range []string{"pools:read", "accounts:delete", "apikeys:list", "audit:read", "users:update", "discovery:create", "settings:write"} {
		if !seen[id] {
			t.Errorf("catalog is missing %q", id)
		}
	}
}

func TestPermissionCatalogReturnsFreshSlice(t *testing.T) {
	first := PermissionCatalog()
	original := first[0]
	first[0] = PermissionDefinition{ID: "tampered", Resource: "tampered", Action: "tampered"}

	second := PermissionCatalog()
	if second[0].ID == "tampered" {
		t.Error("mutating the returned catalog leaked into the next call")
	}
	if second[0] != original {
		t.Errorf("catalog[0] = %+v, want %+v", second[0], original)
	}
}

func TestEveryStaticRolePermissionIsInTheCatalog(t *testing.T) {
	for _, role := range ValidRoles() {
		for _, perm := range GetStaticPermissions(role) {
			if !IsValidPermission(perm) {
				t.Errorf("role %q grants %q which is not in the permission catalog", role, perm.String())
			}
		}
	}
}

func TestPermissionFromIDTable(t *testing.T) {
	tests := []struct {
		id       string
		wantOK   bool
		wantPerm Permission
	}{
		{"pools:read", true, Permission{ResourcePools, ActionRead}},
		{"  settings:write  ", true, Permission{ResourceSettings, ActionWrite}},
		{"apikeys:delete", true, Permission{ResourceAPIKeys, ActionDelete}},
		{"pools", false, Permission{}},
		{"", false, Permission{}},
		{"pools:", false, Permission{Resource: ResourcePools}},
		{":read", false, Permission{Action: ActionRead}},
		{"pools:execute", false, Permission{ResourcePools, "execute"}},
		{"secrets:read", false, Permission{"secrets", ActionRead}},
		{"keys:read", false, Permission{"keys", ActionRead}}, // "keys" is a scope alias, not a resource
		{"POOLS:READ", false, Permission{"POOLS", "READ"}},
		{"pools:read:extra", false, Permission{ResourcePools, "read:extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			perm, ok := PermissionFromID(tt.id)
			if ok != tt.wantOK {
				t.Errorf("PermissionFromID(%q) ok = %v, want %v", tt.id, ok, tt.wantOK)
			}
			if perm != tt.wantPerm {
				t.Errorf("PermissionFromID(%q) = %+v, want %+v", tt.id, perm, tt.wantPerm)
			}
		})
	}
}

func TestIsValidPermissionTable(t *testing.T) {
	tests := []struct {
		name string
		perm Permission
		want bool
	}{
		{"pools read", Permission{ResourcePools, ActionRead}, true},
		{"settings write", Permission{ResourceSettings, ActionWrite}, true},
		{"settings has no create", Permission{ResourceSettings, ActionCreate}, false},
		{"audit has no write", Permission{ResourceAudit, ActionWrite}, false},
		{"audit has no create", Permission{ResourceAudit, ActionCreate}, false},
		{"pools has no write action", Permission{ResourcePools, ActionWrite}, false},
		{"unknown resource", Permission{"secrets", ActionRead}, false},
		{"empty", Permission{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPermission(tt.perm); got != tt.want {
				t.Errorf("IsValidPermission(%+v) = %v, want %v", tt.perm, got, tt.want)
			}
		})
	}
}

func TestValidatePermissionsTable(t *testing.T) {
	tests := []struct {
		name    string
		perms   []Permission
		wantErr bool
	}{
		{"nil accepted", nil, false},
		{"empty accepted", []Permission{}, false},
		{"single valid", []Permission{{ResourcePools, ActionRead}}, false},
		{"duplicates tolerated", []Permission{{ResourcePools, ActionRead}, {ResourcePools, ActionRead}}, false},
		{"one invalid rejects the set", []Permission{{ResourcePools, ActionRead}, {"secrets", ActionRead}}, true},
		{"invalid action rejected", []Permission{{ResourcePools, "explode"}}, true},
		{"zero value rejected", []Permission{{}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePermissions(tt.perms)
			if tt.wantErr && err == nil {
				t.Error("expected ErrInvalidPermission, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestScopeAllowsPermissionMatrix(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		resource string
		action   string
		want     bool
	}{
		{"wildcard allows everything", "*", ResourceSettings, ActionWrite, true},
		{"wildcard is trimmed", "  *  ", ResourcePools, ActionDelete, true},
		{"resource wildcard allows any action", "pools:*", ResourcePools, ActionDelete, true},
		{"resource wildcard does not cross resources", "pools:*", ResourceAccounts, ActionRead, false},
		{"read allows read", "pools:read", ResourcePools, ActionRead, true},
		{"read allows list", "pools:read", ResourcePools, ActionList, true},
		{"read denies create", "pools:read", ResourcePools, ActionCreate, false},
		{"read denies update", "pools:read", ResourcePools, ActionUpdate, false},
		{"read denies delete", "pools:read", ResourcePools, ActionDelete, false},
		{"write allows create", "pools:write", ResourcePools, ActionCreate, true},
		{"write allows read", "pools:write", ResourcePools, ActionRead, true},
		{"write allows update", "pools:write", ResourcePools, ActionUpdate, true},
		{"write allows delete", "pools:write", ResourcePools, ActionDelete, true},
		{"write allows list", "pools:write", ResourcePools, ActionList, true},
		{"write allows the write action", "settings:write", ResourceSettings, ActionWrite, true},
		{"keys alias maps to apikeys", "keys:read", ResourceAPIKeys, ActionRead, true},
		{"keys alias write maps to apikeys", "keys:write", ResourceAPIKeys, ActionDelete, true},
		{"keys alias does not match literal keys", "keys:read", "keys", ActionRead, false},
		{"exact action match", "pools:execute", ResourcePools, "execute", true},
		{"exact action mismatch", "pools:execute", ResourcePools, ActionRead, false},
		{"malformed scope denied", "pools", ResourcePools, ActionRead, false},
		{"empty scope denied", "", ResourcePools, ActionRead, false},
		{"resource mismatch denied", "accounts:write", ResourcePools, ActionRead, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScopeAllowsPermission(tt.scope, tt.resource, tt.action); got != tt.want {
				t.Errorf("ScopeAllowsPermission(%q, %q, %q) = %v, want %v", tt.scope, tt.resource, tt.action, got, tt.want)
			}
		})
	}
}

func TestScopesAllowPermissionEmptyScopeSet(t *testing.T) {
	if ScopesAllowPermission(nil, ResourcePools, ActionRead) {
		t.Error("nil scope set must not grant anything")
	}
	if ScopesAllowPermission([]string{}, ResourcePools, ActionRead) {
		t.Error("empty scope set must not grant anything")
	}
	if !ScopesAllowPermission([]string{"accounts:read", "pools:read"}, ResourcePools, ActionRead) {
		t.Error("any matching scope in the set should grant")
	}
}

func TestPermissionsFromScopes(t *testing.T) {
	permIDs := func(perms []Permission) map[string]bool {
		out := make(map[string]bool, len(perms))
		for _, p := range perms {
			out[p.String()] = true
		}
		return out
	}

	t.Run("no scopes grant nothing", func(t *testing.T) {
		if got := PermissionsFromScopes(nil); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
		if got := PermissionsFromScopes([]string{"bogus"}); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	t.Run("wildcard grants the whole catalog", func(t *testing.T) {
		got := PermissionsFromScopes([]string{"*"})
		if len(got) != len(PermissionCatalog()) {
			t.Errorf("got %d permissions, want %d", len(got), len(PermissionCatalog()))
		}
	})

	t.Run("read scope grants read and list only", func(t *testing.T) {
		ids := permIDs(PermissionsFromScopes([]string{"pools:read"}))
		want := []string{"pools:read", "pools:list"}
		if len(ids) != len(want) {
			t.Errorf("got %v, want exactly %v", ids, want)
		}
		for _, id := range want {
			if !ids[id] {
				t.Errorf("missing %q", id)
			}
		}
	})

	t.Run("keys alias resolves to apikeys permissions", func(t *testing.T) {
		ids := permIDs(PermissionsFromScopes([]string{"keys:write"}))
		for _, id := range []string{"apikeys:create", "apikeys:read", "apikeys:update", "apikeys:delete", "apikeys:list"} {
			if !ids[id] {
				t.Errorf("missing %q", id)
			}
		}
		if ids["pools:read"] {
			t.Error("keys:write must not grant pool permissions")
		}
	})

	t.Run("scopes union", func(t *testing.T) {
		ids := permIDs(PermissionsFromScopes([]string{"pools:read", "audit:read"}))
		for _, id := range []string{"pools:read", "pools:list", "audit:read", "audit:list"} {
			if !ids[id] {
				t.Errorf("missing %q", id)
			}
		}
		if ids["pools:delete"] {
			t.Error("read scopes must not grant delete")
		}
	})
}

func TestRoleLevelOrdering(t *testing.T) {
	tests := []struct {
		role Role
		want int
	}{
		{RoleAdmin, 4},
		{RoleOperator, 3},
		{RoleViewer, 2},
		{RoleAuditor, 1},
		{RoleNone, 0},
		{Role("netops"), 0},
		{Role("ADMIN"), 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if got := RoleLevel(tt.role); got != tt.want {
				t.Errorf("RoleLevel(%q) = %d, want %d", tt.role, got, tt.want)
			}
		})
	}

	if RoleLevel(RoleAdmin) <= RoleLevel(RoleOperator) ||
		RoleLevel(RoleOperator) <= RoleLevel(RoleViewer) ||
		RoleLevel(RoleViewer) <= RoleLevel(RoleAuditor) ||
		RoleLevel(RoleAuditor) <= RoleLevel(RoleNone) {
		t.Error("role levels are not strictly ordered admin > operator > viewer > auditor > none")
	}
}

func TestScopeAllowedByRolePermissions(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		role  Role
		scope string
		want  bool
	}{
		{"admin may issue the full wildcard", RoleAdmin, "*", true},
		{"operator may not issue the full wildcard", RoleOperator, "*", false},
		{"viewer may not issue the full wildcard", RoleViewer, "*", false},
		{"auditor may not issue the full wildcard", RoleAuditor, "*", false},
		{"operator may issue pools:write", RoleOperator, "pools:write", true},
		{"operator may issue pools:*", RoleOperator, "pools:*", true},
		{"operator may issue accounts:write", RoleOperator, "accounts:write", true},
		// operator lacks discovery:delete, so it cannot issue full discovery write.
		{"operator may not issue discovery:write", RoleOperator, "discovery:write", false},
		{"operator may issue discovery:read", RoleOperator, "discovery:read", true},
		{"operator may not issue keys:read", RoleOperator, "keys:read", false},
		{"operator may not issue keys:write", RoleOperator, "keys:write", false},
		{"operator may not issue audit:read", RoleOperator, "audit:read", false},
		{"viewer may issue pools:read", RoleViewer, "pools:read", true},
		{"viewer may not issue pools:write", RoleViewer, "pools:write", false},
		{"viewer may not issue audit:read", RoleViewer, "audit:read", false},
		{"auditor may issue audit:read", RoleAuditor, "audit:read", true},
		{"auditor may not issue pools:read", RoleAuditor, "pools:read", false},
		{"admin may issue keys:write via the alias", RoleAdmin, "keys:write", true},
		{"admin may issue audit:read", RoleAdmin, "audit:read", true},
		{"exact action falls through to the role check", RoleOperator, "pools:update", true},
		{"exact action denied for viewer", RoleViewer, "pools:update", false},
		{"malformed scope denied", RoleAdmin, "pools", false},
		{"empty scope denied", RoleAdmin, "", false},
		{"unknown role denied", Role("netops"), "pools:read", false},
		{"no role denied", RoleNone, "pools:read", false},
		{"whitespace is trimmed", RoleViewer, "  pools:read  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScopeAllowedByRolePermissions(ctx, tt.role, tt.scope); got != tt.want {
				t.Errorf("ScopeAllowedByRolePermissions(%q, %q) = %v, want %v", tt.role, tt.scope, got, tt.want)
			}
		})
	}
}

func TestScopeAllowedByRolePermissionsMatchesDefaultRoleEnvelopes(t *testing.T) {
	// Every scope a role is allowed to issue must actually be covered by
	// that role's own permissions - otherwise key creation escalates.
	ctx := context.Background()
	envelopes := map[Role][]string{
		RoleAdmin:    {"*", "pools:read", "pools:write", "accounts:read", "accounts:write", "keys:read", "keys:write", "discovery:read", "audit:read"},
		RoleOperator: {"pools:read", "pools:write", "accounts:read", "accounts:write", "discovery:read"},
		RoleViewer:   {"pools:read", "accounts:read", "discovery:read"},
		RoleAuditor:  {"audit:read"},
	}

	for role, scopes := range envelopes {
		for _, scope := range scopes {
			if !ScopeAllowedByRolePermissions(ctx, role, scope) {
				t.Errorf("role %q should be able to issue scope %q", role, scope)
			}
		}
	}
}

func TestRoleExistsWithoutProvider(t *testing.T) {
	ctx := context.Background()
	for _, role := range ValidRoles() {
		if !RoleExists(ctx, role) {
			t.Errorf("built-in role %q should exist", role)
		}
	}
	for _, role := range []Role{RoleNone, "netops", "ADMIN"} {
		if RoleExists(ctx, role) {
			t.Errorf("RoleExists(%q) = true with no provider configured", role)
		}
	}
}

func TestIsValidAPIKeyScopeTable(t *testing.T) {
	tests := []struct {
		scope string
		want  bool
	}{
		{"pools:read", true},
		{"pools:write", true},
		{"accounts:read", true},
		{"accounts:write", true},
		{"audit:read", true},
		{"keys:read", true},
		{"keys:write", true},
		{"discovery:read", true},
		{"discovery:write", true},
		{"*", true},
		{"", false},
		{"pools:*", false},
		{"apikeys:read", false},
		{"users:read", false},
		{"settings:write", false},
		{"audit:write", false},
		{" pools:read", false}, // no trimming
		{"POOLS:READ", false},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			if got := IsValidAPIKeyScope(tt.scope); got != tt.want {
				t.Errorf("IsValidAPIKeyScope(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestValidAPIKeyScopesAreAllAccepted(t *testing.T) {
	for _, scope := range ValidAPIKeyScopes {
		if !IsValidAPIKeyScope(scope) {
			t.Errorf("listed scope %q is rejected by IsValidAPIKeyScope", scope)
		}
	}
}

func TestHasPermissionContextAPIKeyScopesCannotExceedThemselves(t *testing.T) {
	future := time.Now().Add(time.Hour)
	key := &APIKey{ID: "k1", Prefix: "cpam_abc", Scopes: []string{"pools:read"}, ExpiresAt: &future}
	ctx := ContextWithAPIKey(context.Background(), key)

	// The role argument must not widen what the key's scopes allow.
	if HasPermissionContext(ctx, RoleAdmin, ResourcePools, ActionCreate) {
		t.Error("an admin role must not widen a pools:read API key to pools:create")
	}
	if HasPermissionContext(ctx, RoleAdmin, ResourceAudit, ActionRead) {
		t.Error("an admin role must not widen a pools:read API key to audit:read")
	}
	if !HasPermissionContext(ctx, RoleViewer, ResourcePools, ActionRead) {
		t.Error("pools:read key should allow pools:read")
	}
	if !HasPermissionContext(ctx, RoleNone, ResourcePools, ActionList) {
		t.Error("pools:read key should allow pools:list")
	}
}

func TestHasPermissionContextExplicitRoleWinsOverAPIKey(t *testing.T) {
	key := &APIKey{ID: "k1", Scopes: []string{"*"}}
	ctx := ContextWithRole(ContextWithAPIKey(context.Background(), key), RoleViewer)

	if HasPermissionContext(ctx, RoleViewer, ResourcePools, ActionDelete) {
		t.Error("explicit viewer role must win over a wildcard API key")
	}
	if !HasPermissionContext(ctx, RoleViewer, ResourcePools, ActionRead) {
		t.Error("explicit viewer role should still allow pools:read")
	}
}

func TestHasPermissionContextInvalidAPIKeyFallsBackToRole(t *testing.T) {
	revoked := &APIKey{ID: "k1", Scopes: []string{"*"}, Revoked: true}
	ctx := ContextWithAPIKey(context.Background(), revoked)

	if HasPermissionContext(ctx, RoleViewer, ResourcePools, ActionDelete) {
		t.Error("a revoked wildcard key must not grant pools:delete")
	}
	if !HasPermissionContext(ctx, RoleViewer, ResourcePools, ActionRead) {
		t.Error("the role should still apply when the key is revoked")
	}

	past := time.Now().Add(-time.Hour)
	expired := &APIKey{ID: "k2", Scopes: []string{"*"}, ExpiresAt: &past}
	ctx = ContextWithAPIKey(context.Background(), expired)
	if HasPermissionContext(ctx, RoleViewer, ResourcePools, ActionDelete) {
		t.Error("an expired wildcard key must not grant pools:delete")
	}
}

func TestHasPermissionContextSessionUsesRoleArgument(t *testing.T) {
	session := &Session{ID: "s1", UserID: "u1", Role: RoleAdmin, ExpiresAt: time.Now().Add(time.Hour)}
	ctx := ContextWithSession(context.Background(), session)

	if !HasPermissionContext(ctx, RoleOperator, ResourcePools, ActionCreate) {
		t.Error("valid session should evaluate the supplied role")
	}
	if HasPermissionContext(ctx, RoleOperator, ResourceAudit, ActionRead) {
		t.Error("operator must not read audit even inside an admin session")
	}
}

func TestGetPermissionsContextSources(t *testing.T) {
	t.Run("api key scopes drive permissions", func(t *testing.T) {
		key := &APIKey{ID: "k1", Scopes: []string{"audit:read"}}
		ctx := ContextWithAPIKey(context.Background(), key)

		perms := GetPermissionsContext(ctx, RoleAdmin)
		if len(perms) != 2 {
			t.Fatalf("got %d permissions (%v), want audit:read and audit:list", len(perms), perms)
		}
		for _, p := range perms {
			if p.Resource != ResourceAudit {
				t.Errorf("unexpected permission %q from an audit:read key", p.String())
			}
		}
	})

	t.Run("explicit role wins over api key", func(t *testing.T) {
		key := &APIKey{ID: "k1", Scopes: []string{"audit:read"}}
		ctx := ContextWithRole(ContextWithAPIKey(context.Background(), key), RoleViewer)

		perms := GetPermissionsContext(ctx, RoleViewer)
		want := GetStaticPermissions(RoleViewer)
		if len(perms) != len(want) {
			t.Fatalf("got %d permissions, want %d", len(perms), len(want))
		}
	})

	t.Run("session falls back to the role", func(t *testing.T) {
		session := &Session{ID: "s1", UserID: "u1", Role: RoleOperator, ExpiresAt: time.Now().Add(time.Hour)}
		ctx := ContextWithSession(context.Background(), session)

		perms := GetPermissionsContext(ctx, RoleOperator)
		if len(perms) != len(GetStaticPermissions(RoleOperator)) {
			t.Errorf("got %d permissions, want the operator role set", len(perms))
		}
	})

	t.Run("bare context falls back to the role", func(t *testing.T) {
		perms := GetPermissionsContext(context.Background(), RoleAuditor)
		if len(perms) != len(GetStaticPermissions(RoleAuditor)) {
			t.Errorf("got %d permissions, want the auditor role set", len(perms))
		}
	})
}

func TestGetStaticPermissionsReturnsIsolatedCopy(t *testing.T) {
	first := GetStaticPermissions(RoleViewer)
	if len(first) == 0 {
		t.Fatal("viewer should have permissions")
	}
	first[0] = Permission{Resource: ResourceUsers, Action: ActionDelete}

	second := GetStaticPermissions(RoleViewer)
	if second[0].Resource == ResourceUsers {
		t.Error("mutating the returned slice corrupted RolePermissions")
	}

	if GetStaticPermissions(Role("netops")) != nil {
		t.Error("unknown roles must have no static permissions")
	}
	if GetStaticPermissions(RoleNone) != nil {
		t.Error("RoleNone must have no static permissions")
	}
}
