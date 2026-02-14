// Package auth provides authentication and authorization for CloudPAM.
package auth

import (
	"strings"
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
)

// Action constants for permission checks.
const (
	ActionCreate = "create"
	ActionRead   = "read"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionList   = "list"
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
var rolePermissionCache map[Role]map[string]map[string]bool

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
	if role == RoleNone {
		return false
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

// GetPermissions returns all permissions for a given role.
// Returns nil for unknown roles.
func GetPermissions(role Role) []Permission {
	perms, ok := RolePermissions[role]
	if !ok {
		return nil
	}
	// Return a copy to prevent mutation
	result := make([]Permission, len(perms))
	copy(result, perms)
	return result
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
