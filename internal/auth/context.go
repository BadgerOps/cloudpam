package auth

import "context"

type contextKey string

const apiKeyContextKey contextKey = "apiKey"

// ContextWithAPIKey returns a new context with the API key stored in it.
func ContextWithAPIKey(ctx context.Context, key *APIKey) context.Context {
	if key == nil {
		return ctx
	}
	return context.WithValue(ctx, apiKeyContextKey, key)
}

// APIKeyFromContext retrieves the API key from the context.
// Returns nil if no API key is present.
func APIKeyFromContext(ctx context.Context) *APIKey {
	if ctx == nil {
		return nil
	}
	key, ok := ctx.Value(apiKeyContextKey).(*APIKey)
	if !ok {
		return nil
	}
	return key
}

// IsAuthenticated returns true if the context contains a valid API key.
func IsAuthenticated(ctx context.Context) bool {
	key := APIKeyFromContext(ctx)
	return key != nil && key.IsValid()
}

// RequireScope checks if the context's API key has the required scope.
// Returns nil if the scope is present, or ErrInsufficientScopes if not.
func RequireScope(ctx context.Context, scope string) error {
	key := APIKeyFromContext(ctx)
	if key == nil {
		return ErrKeyNotFound
	}
	if !key.HasScope(scope) {
		return ErrInsufficientScopes
	}
	return nil
}

// RequireAnyScope checks if the context's API key has any of the required scopes.
// Returns nil if any scope is present, or ErrInsufficientScopes if none match.
func RequireAnyScope(ctx context.Context, scopes ...string) error {
	key := APIKeyFromContext(ctx)
	if key == nil {
		return ErrKeyNotFound
	}
	if !key.HasAnyScope(scopes...) {
		return ErrInsufficientScopes
	}
	return nil
}

// Role context key
const roleContextKey contextKey = "role"

// ContextWithRole returns a new context with the role stored in it.
func ContextWithRole(ctx context.Context, role Role) context.Context {
	return context.WithValue(ctx, roleContextKey, role)
}

// RoleFromContext retrieves the role from the context.
// Returns RoleNone if no role is present.
func RoleFromContext(ctx context.Context) Role {
	if ctx == nil {
		return RoleNone
	}
	role, ok := ctx.Value(roleContextKey).(Role)
	if !ok {
		return RoleNone
	}
	return role
}

// GetEffectiveRole returns the effective role for the current context.
// It first checks for an explicit role, then derives one from the API key scopes.
func GetEffectiveRole(ctx context.Context) Role {
	// Check for explicit role first (e.g., from session)
	if role := RoleFromContext(ctx); role != RoleNone {
		return role
	}

	// Derive role from API key scopes
	if key := APIKeyFromContext(ctx); key != nil && key.IsValid() {
		return GetRoleFromScopes(key.Scopes)
	}

	return RoleNone
}

// RequirePermission checks if the context has permission for a resource action.
// Returns nil if permitted, or ErrInsufficientScopes if not.
func RequirePermission(ctx context.Context, resource, action string) error {
	role := GetEffectiveRole(ctx)
	if !HasPermission(role, resource, action) {
		return ErrInsufficientScopes
	}
	return nil
}
