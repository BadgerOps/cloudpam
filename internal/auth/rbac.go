// Package auth provides authentication and authorization for CloudPAM.
package auth

import (
	"context"
	"strings"
	"sync"
)

// Role represents a user role in the RBAC system.
type Role string

const (
	// RoleAdmin has full access to all resources and operations.
	RoleAdmin Role = "admin"

	// RoleOperator has read/write access to pools and accounts.
	RoleOperator Role = "operator"

	// RoleViewer has read-only access to pools and accounts.
	RoleViewer Role = "viewer"

	// RoleAuditor has access to audit logs only.
	RoleAuditor Role = "auditor"

	// RoleNone represents no role (unauthenticated or unknown).
	RoleNone Role = ""
)

// Resource constants for permission checks.
const (
	ResourcePools     = "pools"
	ResourceAccounts  = "accounts"
	ResourceAPIKeys   = "apikeys"
	ResourceAudit     = "audit"
	ResourceUsers     = "users"
	ResourceDiscovery = "discovery"
	ResourceSettings  = "settings"
)

// Action constants for permission checks.
const (
	ActionCreate = "create"
	ActionRead   = "read"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionList   = "list"
	ActionWrite  = "write"
)

// Permission represents an action on a resource.
type Permission struct {
	Resource string // "pools", "accounts", "apikeys", "audit"
	Action   string // "create", "read", "update", "delete", "list"
}

// String returns a string representation of the permission (e.g., "pools:read").
func (p Permission) String() string {
	return p.Resource + ":" + p.Action
}

// PermissionCatalog returns the complete app-wide permission catalog.
func PermissionCatalog() []PermissionDefinition {
	defs := []PermissionDefinition{
		{ID: "pools:create", Resource: ResourcePools, Action: ActionCreate, Name: "Create pools", Description: "Create address pools and planned allocations.", Category: "IPAM"},
		{ID: "pools:read", Resource: ResourcePools, Action: ActionRead, Name: "Read pools", Description: "View pool details, blocks, utilization, and schema checks.", Category: "IPAM"},
		{ID: "pools:update", Resource: ResourcePools, Action: ActionUpdate, Name: "Update pools", Description: "Edit pool metadata, hierarchy, and assignment.", Category: "IPAM"},
		{ID: "pools:delete", Resource: ResourcePools, Action: ActionDelete, Name: "Delete pools", Description: "Delete pools and planned allocations.", Category: "IPAM"},
		{ID: "pools:list", Resource: ResourcePools, Action: ActionList, Name: "List pools", Description: "Browse pool lists and tree views.", Category: "IPAM"},
		{ID: "accounts:create", Resource: ResourceAccounts, Action: ActionCreate, Name: "Create accounts", Description: "Create cloud account records.", Category: "Accounts"},
		{ID: "accounts:read", Resource: ResourceAccounts, Action: ActionRead, Name: "Read accounts", Description: "View account details and account-linked resources.", Category: "Accounts"},
		{ID: "accounts:update", Resource: ResourceAccounts, Action: ActionUpdate, Name: "Update accounts", Description: "Edit account metadata.", Category: "Accounts"},
		{ID: "accounts:delete", Resource: ResourceAccounts, Action: ActionDelete, Name: "Delete accounts", Description: "Delete account records.", Category: "Accounts"},
		{ID: "accounts:list", Resource: ResourceAccounts, Action: ActionList, Name: "List accounts", Description: "Browse account lists.", Category: "Accounts"},
		{ID: "apikeys:create", Resource: ResourceAPIKeys, Action: ActionCreate, Name: "Create API keys", Description: "Create API tokens within the caller's permission envelope.", Category: "Identity"},
		{ID: "apikeys:read", Resource: ResourceAPIKeys, Action: ActionRead, Name: "Read API keys", Description: "View API key metadata.", Category: "Identity"},
		{ID: "apikeys:update", Resource: ResourceAPIKeys, Action: ActionUpdate, Name: "Update API keys", Description: "Reserved for future API key metadata updates.", Category: "Identity"},
		{ID: "apikeys:delete", Resource: ResourceAPIKeys, Action: ActionDelete, Name: "Delete API keys", Description: "Revoke API keys.", Category: "Identity"},
		{ID: "apikeys:list", Resource: ResourceAPIKeys, Action: ActionList, Name: "List API keys", Description: "Browse API key metadata.", Category: "Identity"},
		{ID: "audit:read", Resource: ResourceAudit, Action: ActionRead, Name: "Read audit logs", Description: "View audit event details.", Category: "Audit"},
		{ID: "audit:list", Resource: ResourceAudit, Action: ActionList, Name: "List audit logs", Description: "Browse audit events.", Category: "Audit"},
		{ID: "users:create", Resource: ResourceUsers, Action: ActionCreate, Name: "Create users", Description: "Create local user accounts.", Category: "Identity"},
		{ID: "users:read", Resource: ResourceUsers, Action: ActionRead, Name: "Read users", Description: "View user account details.", Category: "Identity"},
		{ID: "users:update", Resource: ResourceUsers, Action: ActionUpdate, Name: "Update users", Description: "Edit users, roles, password state, and active status.", Category: "Identity"},
		{ID: "users:delete", Resource: ResourceUsers, Action: ActionDelete, Name: "Delete users", Description: "Deactivate user accounts.", Category: "Identity"},
		{ID: "users:list", Resource: ResourceUsers, Action: ActionList, Name: "List users", Description: "Browse user accounts.", Category: "Identity"},
		{ID: "discovery:create", Resource: ResourceDiscovery, Action: ActionCreate, Name: "Start discovery", Description: "Start discovery syncs and register agents.", Category: "Discovery"},
		{ID: "discovery:read", Resource: ResourceDiscovery, Action: ActionRead, Name: "Read discovery", Description: "View discovered resources, agents, drift, and recommendations.", Category: "Discovery"},
		{ID: "discovery:update", Resource: ResourceDiscovery, Action: ActionUpdate, Name: "Update discovery", Description: "Apply discovery results and reconcile drift.", Category: "Discovery"},
		{ID: "discovery:delete", Resource: ResourceDiscovery, Action: ActionDelete, Name: "Delete discovery", Description: "Reserved for future discovery cleanup operations.", Category: "Discovery"},
		{ID: "discovery:list", Resource: ResourceDiscovery, Action: ActionList, Name: "List discovery", Description: "Browse discovery resources, jobs, agents, drift, and recommendations.", Category: "Discovery"},
		{ID: "settings:read", Resource: ResourceSettings, Action: ActionRead, Name: "Read settings", Description: "View security, OIDC, update, and system configuration.", Category: "Settings"},
		{ID: "settings:write", Resource: ResourceSettings, Action: ActionWrite, Name: "Write settings", Description: "Change security, OIDC, update, and system configuration.", Category: "Settings"},
	}
	result := make([]PermissionDefinition, len(defs))
	copy(result, defs)
	return result
}

func PermissionFromID(id string) (Permission, bool) {
	parts := strings.SplitN(strings.TrimSpace(id), ":", 2)
	if len(parts) != 2 {
		return Permission{}, false
	}
	perm := Permission{Resource: parts[0], Action: parts[1]}
	return perm, IsValidPermission(perm)
}

func IsValidPermission(perm Permission) bool {
	id := perm.String()
	for _, def := range PermissionCatalog() {
		if def.ID == id {
			return true
		}
	}
	return false
}

func ValidatePermissions(perms []Permission) error {
	seen := make(map[string]bool, len(perms))
	for _, perm := range perms {
		if !IsValidPermission(perm) {
			return ErrInvalidPermission
		}
		if seen[perm.String()] {
			continue
		}
		seen[perm.String()] = true
	}
	return nil
}

// RolePermissions maps roles to their allowed permissions.
// This is the authoritative source of what each role can do.
var RolePermissions = map[Role][]Permission{
	RoleAdmin: {
		// Full access to all resources
		{ResourcePools, ActionCreate},
		{ResourcePools, ActionRead},
		{ResourcePools, ActionUpdate},
		{ResourcePools, ActionDelete},
		{ResourcePools, ActionList},
		{ResourceAccounts, ActionCreate},
		{ResourceAccounts, ActionRead},
		{ResourceAccounts, ActionUpdate},
		{ResourceAccounts, ActionDelete},
		{ResourceAccounts, ActionList},
		{ResourceAPIKeys, ActionCreate},
		{ResourceAPIKeys, ActionRead},
		{ResourceAPIKeys, ActionUpdate},
		{ResourceAPIKeys, ActionDelete},
		{ResourceAPIKeys, ActionList},
		{ResourceAudit, ActionRead},
		{ResourceAudit, ActionList},
		{ResourceUsers, ActionCreate},
		{ResourceUsers, ActionRead},
		{ResourceUsers, ActionUpdate},
		{ResourceUsers, ActionDelete},
		{ResourceUsers, ActionList},
		{ResourceDiscovery, ActionCreate},
		{ResourceDiscovery, ActionRead},
		{ResourceDiscovery, ActionUpdate},
		{ResourceDiscovery, ActionDelete},
		{ResourceDiscovery, ActionList},
		{ResourceSettings, ActionRead},
		{ResourceSettings, ActionWrite},
	},
	RoleOperator: {
		// Read/write access to pools, accounts, and discovery
		{ResourcePools, ActionCreate},
		{ResourcePools, ActionRead},
		{ResourcePools, ActionUpdate},
		{ResourcePools, ActionDelete},
		{ResourcePools, ActionList},
		{ResourceAccounts, ActionCreate},
		{ResourceAccounts, ActionRead},
		{ResourceAccounts, ActionUpdate},
		{ResourceAccounts, ActionDelete},
		{ResourceAccounts, ActionList},
		{ResourceDiscovery, ActionCreate},
		{ResourceDiscovery, ActionRead},
		{ResourceDiscovery, ActionUpdate},
		{ResourceDiscovery, ActionList},
	},
	RoleViewer: {
		// Read-only access to pools, accounts, and discovery
		{ResourcePools, ActionRead},
		{ResourcePools, ActionList},
		{ResourceAccounts, ActionRead},
		{ResourceAccounts, ActionList},
		{ResourceDiscovery, ActionRead},
		{ResourceDiscovery, ActionList},
	},
	RoleAuditor: {
		// Access to audit logs only
		{ResourceAudit, ActionRead},
		{ResourceAudit, ActionList},
	},
}

// rolePermissionCache is a pre-computed lookup table for faster permission checks.
// Map format: role -> resource -> action -> bool
var (
	rolePermissionCache map[Role]map[string]map[string]bool
	roleProviderMu      sync.RWMutex
	roleProvider        RoleStore
)

func init() {
	rolePermissionCache = make(map[Role]map[string]map[string]bool)
	for role, perms := range RolePermissions {
		rolePermissionCache[role] = make(map[string]map[string]bool)
		for _, perm := range perms {
			if rolePermissionCache[role][perm.Resource] == nil {
				rolePermissionCache[role][perm.Resource] = make(map[string]bool)
			}
			rolePermissionCache[role][perm.Resource][perm.Action] = true
		}
	}
}

// HasPermission checks if a role has permission for a specific resource and action.
// Returns false for unknown roles or permissions (default deny).
func HasPermission(role Role, resource, action string) bool {
	return HasPermissionContext(context.Background(), role, resource, action)
}

func HasPermissionContext(ctx context.Context, role Role, resource, action string) bool {
	if key := APIKeyFromContext(ctx); key != nil && key.IsValid() {
		return ScopesAllowPermission(key.Scopes, resource, action)
	}
	if role == RoleNone {
		return false
	}
	if provider := currentRoleProvider(); provider != nil {
		def, err := provider.GetRole(ctx, role)
		if err == nil && def != nil {
			for _, perm := range def.Permissions {
				if perm.Resource == resource && perm.Action == action {
					return true
				}
			}
			return false
		}
	}

	resourcePerms, ok := rolePermissionCache[role]
	if !ok {
		return false // Unknown role - deny
	}

	actionPerms, ok := resourcePerms[resource]
	if !ok {
		return false // Unknown resource - deny
	}

	return actionPerms[action] // Unknown action returns false (deny)
}

func SetRoleStoreProvider(provider RoleStore) {
	roleProviderMu.Lock()
	defer roleProviderMu.Unlock()
	roleProvider = provider
}

func currentRoleProvider() RoleStore {
	roleProviderMu.RLock()
	defer roleProviderMu.RUnlock()
	return roleProvider
}

// GetPermissions returns all permissions for a given role.
// Returns nil for unknown roles.
func GetPermissions(role Role) []Permission {
	return GetPermissionsContext(context.Background(), role)
}

func GetPermissionsContext(ctx context.Context, role Role) []Permission {
	if key := APIKeyFromContext(ctx); key != nil && key.IsValid() {
		return PermissionsFromScopes(key.Scopes)
	}
	if provider := currentRoleProvider(); provider != nil {
		def, err := provider.GetRole(ctx, role)
		if err == nil && def != nil {
			return copyPermissions(def.Permissions)
		}
	}
	return GetStaticPermissions(role)
}

func ScopesAllowPermission(scopes []string, resource, action string) bool {
	for _, scope := range scopes {
		if ScopeAllowsPermission(scope, resource, action) {
			return true
		}
	}
	return false
}

func ScopeAllowsPermission(scope, resource, action string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "*" {
		return true
	}
	parts := strings.SplitN(scope, ":", 2)
	if len(parts) != 2 {
		return false
	}
	scopeResource := parts[0]
	if scopeResource == "keys" {
		scopeResource = ResourceAPIKeys
	}
	if scopeResource != resource {
		return false
	}
	switch parts[1] {
	case "*":
		return true
	case "read":
		return action == ActionRead || action == ActionList
	case "write":
		return action == ActionCreate || action == ActionRead || action == ActionUpdate || action == ActionDelete || action == ActionList || action == ActionWrite
	default:
		return parts[1] == action
	}
}

func PermissionsFromScopes(scopes []string) []Permission {
	var perms []Permission
	for _, def := range PermissionCatalog() {
		if ScopesAllowPermission(scopes, def.Resource, def.Action) {
			perms = append(perms, Permission{Resource: def.Resource, Action: def.Action})
		}
	}
	return perms
}

func GetStaticPermissions(role Role) []Permission {
	perms, ok := RolePermissions[role]
	if !ok {
		return nil
	}
	// Return a copy to prevent mutation
	result := make([]Permission, len(perms))
	copy(result, perms)
	return result
}

func copyPermissions(perms []Permission) []Permission {
	result := make([]Permission, len(perms))
	copy(result, perms)
	return result
}

func RoleExists(ctx context.Context, role Role) bool {
	if IsBuiltinRole(role) {
		return true
	}
	if provider := currentRoleProvider(); provider != nil {
		def, err := provider.GetRole(ctx, role)
		return err == nil && def != nil
	}
	return false
}

func ScopeAllowedByRolePermissions(ctx context.Context, role Role, scope string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "*" {
		for _, def := range PermissionCatalog() {
			if !HasPermissionContext(ctx, role, def.Resource, def.Action) {
				return false
			}
		}
		return true
	}
	parts := strings.SplitN(scope, ":", 2)
	if len(parts) != 2 {
		return false
	}
	resource := parts[0]
	action := parts[1]
	if resource == "keys" {
		resource = ResourceAPIKeys
	}
	if action == "read" {
		return HasPermissionContext(ctx, role, resource, ActionRead) || HasPermissionContext(ctx, role, resource, ActionList)
	}
	if action == "write" || action == "*" {
		return HasPermissionContext(ctx, role, resource, ActionCreate) &&
			HasPermissionContext(ctx, role, resource, ActionRead) &&
			HasPermissionContext(ctx, role, resource, ActionUpdate) &&
			HasPermissionContext(ctx, role, resource, ActionDelete) &&
			HasPermissionContext(ctx, role, resource, ActionList)
	}
	return HasPermissionContext(ctx, role, resource, action)
}

// GetRoleFromScopes determines the effective role based on API key scopes.
// The role is determined by the highest privilege level implied by the scopes.
//
// Scope mapping:
//   - "*" -> admin
//   - "pools:write" or "accounts:write" or "keys:write" -> operator
//   - "pools:read" or "accounts:read" -> viewer
//   - "audit:read" -> auditor
//
// If multiple role-implying scopes are present, the highest privilege wins.
func GetRoleFromScopes(scopes []string) Role {
	hasAdmin := false
	hasWrite := false
	hasRead := false
	hasAudit := false

	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)

		// Admin scope
		if scope == "*" {
			hasAdmin = true
			continue
		}

		// Check for wildcard resource scopes (e.g., "pools:*")
		if strings.HasSuffix(scope, ":*") {
			resource := strings.TrimSuffix(scope, ":*")
			switch resource {
			case "pools", "accounts", "keys", "discovery":
				hasWrite = true
			case "audit":
				hasAudit = true
			}
			continue
		}

		// Parse resource:action format
		parts := strings.SplitN(scope, ":", 2)
		if len(parts) != 2 {
			continue
		}
		resource := parts[0]
		action := parts[1]

		switch resource {
		case "pools", "accounts", "keys", "discovery":
			switch action {
			case "write":
				hasWrite = true
			case "read":
				hasRead = true
			}
		case "audit":
			if action == "read" {
				hasAudit = true
			}
		}
	}

	// Return highest privilege level
	switch {
	case hasAdmin:
		return RoleAdmin
	case hasWrite:
		return RoleOperator
	case hasRead:
		return RoleViewer
	case hasAudit:
		return RoleAuditor
	default:
		return RoleNone
	}
}

// RoleLevel returns the numeric privilege level of a role for comparison.
// Higher values = more privileges.
func RoleLevel(r Role) int {
	switch r {
	case RoleAdmin:
		return 4
	case RoleOperator:
		return 3
	case RoleViewer:
		return 2
	case RoleAuditor:
		return 1
	default:
		return 0
	}
}

// ValidRoles returns all valid role values.
func ValidRoles() []Role {
	return []Role{RoleAdmin, RoleOperator, RoleViewer, RoleAuditor}
}

// IsValidRole returns true if the given role is a valid defined role.
func IsValidRole(role Role) bool {
	switch role {
	case RoleAdmin, RoleOperator, RoleViewer, RoleAuditor:
		return true
	default:
		return false
	}
}

// ParseRole parses a string into a Role.
// Returns RoleNone if the string doesn't match a valid role.
func ParseRole(s string) Role {
	role := Role(strings.ToLower(strings.TrimSpace(s)))
	if IsValidRole(role) {
		return role
	}
	return RoleNone
}
