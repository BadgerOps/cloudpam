package oidc

import "cloudpam/internal/auth"

// Claims represents extracted OIDC ID token claims.
type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Groups  []string `json:"groups"`
	Issuer  string   `json:"iss"`
}

// MapRole evaluates group-to-role mapping rules against claims.
// mapping: IdP group name -> CloudPAM role string (e.g., "cloudpam-admins" -> "admin")
// Returns the highest-privilege matching role, or defaultRole if no match.
func MapRole(claims Claims, mapping map[string]string, defaultRole auth.Role) auth.Role {
	bestRole := auth.RoleNone
	for _, group := range claims.Groups {
		if roleStr, ok := mapping[group]; ok {
			role := auth.ParseRole(roleStr)
			if auth.RoleLevel(role) > auth.RoleLevel(bestRole) {
				bestRole = role
			}
		}
	}
	if bestRole == auth.RoleNone {
		return defaultRole
	}
	return bestRole
}
