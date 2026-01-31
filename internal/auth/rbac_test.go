package auth

import (
	"testing"
)

func TestHasPermission(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		resource string
		action   string
		want     bool
	}{
		// Admin - full access
		{"admin can create pools", RoleAdmin, ResourcePools, ActionCreate, true},
		{"admin can read pools", RoleAdmin, ResourcePools, ActionRead, true},
		{"admin can update pools", RoleAdmin, ResourcePools, ActionUpdate, true},
		{"admin can delete pools", RoleAdmin, ResourcePools, ActionDelete, true},
		{"admin can list pools", RoleAdmin, ResourcePools, ActionList, true},
		{"admin can create accounts", RoleAdmin, ResourceAccounts, ActionCreate, true},
		{"admin can read accounts", RoleAdmin, ResourceAccounts, ActionRead, true},
		{"admin can create apikeys", RoleAdmin, ResourceAPIKeys, ActionCreate, true},
		{"admin can delete apikeys", RoleAdmin, ResourceAPIKeys, ActionDelete, true},
		{"admin can read audit", RoleAdmin, ResourceAudit, ActionRead, true},

		// Operator - pools and accounts only
		{"operator can create pools", RoleOperator, ResourcePools, ActionCreate, true},
		{"operator can read pools", RoleOperator, ResourcePools, ActionRead, true},
		{"operator can update pools", RoleOperator, ResourcePools, ActionUpdate, true},
		{"operator can delete pools", RoleOperator, ResourcePools, ActionDelete, true},
		{"operator can create accounts", RoleOperator, ResourceAccounts, ActionCreate, true},
		{"operator cannot create apikeys", RoleOperator, ResourceAPIKeys, ActionCreate, false},
		{"operator cannot read audit", RoleOperator, ResourceAudit, ActionRead, false},

		// Viewer - read only
		{"viewer can read pools", RoleViewer, ResourcePools, ActionRead, true},
		{"viewer can list pools", RoleViewer, ResourcePools, ActionList, true},
		{"viewer cannot create pools", RoleViewer, ResourcePools, ActionCreate, false},
		{"viewer cannot update pools", RoleViewer, ResourcePools, ActionUpdate, false},
		{"viewer cannot delete pools", RoleViewer, ResourcePools, ActionDelete, false},
		{"viewer can read accounts", RoleViewer, ResourceAccounts, ActionRead, true},
		{"viewer cannot create accounts", RoleViewer, ResourceAccounts, ActionCreate, false},
		{"viewer cannot read apikeys", RoleViewer, ResourceAPIKeys, ActionRead, false},
		{"viewer cannot read audit", RoleViewer, ResourceAudit, ActionRead, false},

		// Auditor - audit only
		{"auditor can read audit", RoleAuditor, ResourceAudit, ActionRead, true},
		{"auditor can list audit", RoleAuditor, ResourceAudit, ActionList, true},
		{"auditor cannot read pools", RoleAuditor, ResourcePools, ActionRead, false},
		{"auditor cannot read accounts", RoleAuditor, ResourceAccounts, ActionRead, false},
		{"auditor cannot read apikeys", RoleAuditor, ResourceAPIKeys, ActionRead, false},

		// RoleNone - no access
		{"none cannot read pools", RoleNone, ResourcePools, ActionRead, false},
		{"none cannot read accounts", RoleNone, ResourceAccounts, ActionRead, false},
		{"none cannot read audit", RoleNone, ResourceAudit, ActionRead, false},

		// Unknown role - no access (default deny)
		{"unknown role denied", Role("superuser"), ResourcePools, ActionRead, false},

		// Unknown resource - no access (default deny)
		{"unknown resource denied", RoleAdmin, "secrets", ActionRead, false},

		// Unknown action - no access (default deny)
		{"unknown action denied", RoleAdmin, ResourcePools, "execute", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasPermission(tt.role, tt.resource, tt.action)
			if got != tt.want {
				t.Errorf("HasPermission(%q, %q, %q) = %v, want %v",
					tt.role, tt.resource, tt.action, got, tt.want)
			}
		})
	}
}

func TestGetPermissions(t *testing.T) {
	// Admin should have the most permissions
	adminPerms := GetPermissions(RoleAdmin)
	if len(adminPerms) == 0 {
		t.Error("Admin should have permissions")
	}

	// Check admin has specific permissions
	hasAuditRead := false
	hasAPIKeysCreate := false
	for _, p := range adminPerms {
		if p.Resource == ResourceAudit && p.Action == ActionRead {
			hasAuditRead = true
		}
		if p.Resource == ResourceAPIKeys && p.Action == ActionCreate {
			hasAPIKeysCreate = true
		}
	}
	if !hasAuditRead {
		t.Error("Admin should have audit:read permission")
	}
	if !hasAPIKeysCreate {
		t.Error("Admin should have apikeys:create permission")
	}

	// Viewer should have fewer permissions
	viewerPerms := GetPermissions(RoleViewer)
	if len(viewerPerms) >= len(adminPerms) {
		t.Error("Viewer should have fewer permissions than admin")
	}

	// Unknown role should return nil
	unknownPerms := GetPermissions(Role("unknown"))
	if unknownPerms != nil {
		t.Error("Unknown role should return nil permissions")
	}

	// Ensure returned permissions are a copy
	operatorPerms := GetPermissions(RoleOperator)
	if len(operatorPerms) > 0 {
		operatorPerms[0] = Permission{Resource: "modified", Action: "modified"}
		origPerms := GetPermissions(RoleOperator)
		if origPerms[0].Resource == "modified" {
			t.Error("GetPermissions should return a copy, not the original")
		}
	}
}

func TestGetRoleFromScopes(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		want   Role
	}{
		// Admin scope
		{"admin wildcard", []string{"*"}, RoleAdmin},
		{"admin with other scopes", []string{"pools:read", "*"}, RoleAdmin},

		// Operator scopes (write access)
		{"pools write", []string{"pools:write"}, RoleOperator},
		{"accounts write", []string{"accounts:write"}, RoleOperator},
		{"keys write", []string{"keys:write"}, RoleOperator},
		{"pools wildcard", []string{"pools:*"}, RoleOperator},
		{"multiple write scopes", []string{"pools:write", "accounts:write"}, RoleOperator},

		// Viewer scopes (read only)
		{"pools read", []string{"pools:read"}, RoleViewer},
		{"accounts read", []string{"accounts:read"}, RoleViewer},
		{"keys read", []string{"keys:read"}, RoleViewer},
		{"multiple read scopes", []string{"pools:read", "accounts:read"}, RoleViewer},

		// Auditor scopes
		{"audit read", []string{"audit:read"}, RoleAuditor},
		{"audit wildcard", []string{"audit:*"}, RoleAuditor},

		// No valid scopes
		{"empty scopes", []string{}, RoleNone},
		{"invalid scopes", []string{"invalid", "unknown:scope"}, RoleNone},
		{"nil scopes", nil, RoleNone},

		// Mixed scopes - highest privilege wins
		{"read and write", []string{"pools:read", "pools:write"}, RoleOperator},
		{"viewer and auditor", []string{"pools:read", "audit:read"}, RoleViewer},
		{"auditor only has audit", []string{"audit:read"}, RoleAuditor},

		// Whitespace handling
		{"scope with spaces", []string{" pools:read ", "  accounts:read"}, RoleViewer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetRoleFromScopes(tt.scopes)
			if got != tt.want {
				t.Errorf("GetRoleFromScopes(%v) = %q, want %q", tt.scopes, got, tt.want)
			}
		})
	}
}

func TestValidRoles(t *testing.T) {
	roles := ValidRoles()
	expected := []Role{RoleAdmin, RoleOperator, RoleViewer, RoleAuditor}

	if len(roles) != len(expected) {
		t.Errorf("ValidRoles() returned %d roles, want %d", len(roles), len(expected))
	}

	roleSet := make(map[Role]bool)
	for _, r := range roles {
		roleSet[r] = true
	}

	for _, e := range expected {
		if !roleSet[e] {
			t.Errorf("ValidRoles() missing expected role %q", e)
		}
	}
}

func TestIsValidRole(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleAdmin, true},
		{RoleOperator, true},
		{RoleViewer, true},
		{RoleAuditor, true},
		{RoleNone, false},
		{Role("superuser"), false},
		{Role(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got := IsValidRole(tt.role)
			if got != tt.want {
				t.Errorf("IsValidRole(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		input string
		want  Role
	}{
		{"admin", RoleAdmin},
		{"ADMIN", RoleAdmin},
		{"Admin", RoleAdmin},
		{"  admin  ", RoleAdmin},
		{"operator", RoleOperator},
		{"viewer", RoleViewer},
		{"auditor", RoleAuditor},
		{"", RoleNone},
		{"invalid", RoleNone},
		{"superuser", RoleNone},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseRole(tt.input)
			if got != tt.want {
				t.Errorf("ParseRole(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPermissionString(t *testing.T) {
	tests := []struct {
		perm Permission
		want string
	}{
		{Permission{Resource: "pools", Action: "read"}, "pools:read"},
		{Permission{Resource: "accounts", Action: "create"}, "accounts:create"},
		{Permission{Resource: "apikeys", Action: "delete"}, "apikeys:delete"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.perm.String()
			if got != tt.want {
				t.Errorf("Permission.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRolePermissionCacheConsistency(t *testing.T) {
	// Verify that the cache is consistent with the source RolePermissions map
	for role, perms := range RolePermissions {
		for _, perm := range perms {
			if !HasPermission(role, perm.Resource, perm.Action) {
				t.Errorf("Cache inconsistent: role %q should have permission %s:%s",
					role, perm.Resource, perm.Action)
			}
		}
	}
}

// Benchmark permission checking
func BenchmarkHasPermission(b *testing.B) {
	for i := 0; i < b.N; i++ {
		HasPermission(RoleAdmin, ResourcePools, ActionRead)
	}
}

func BenchmarkGetRoleFromScopes(b *testing.B) {
	scopes := []string{"pools:read", "pools:write", "accounts:read"}
	for i := 0; i < b.N; i++ {
		GetRoleFromScopes(scopes)
	}
}
