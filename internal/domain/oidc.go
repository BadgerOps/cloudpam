package domain

import "time"

// OIDCProvider represents a configured OIDC identity provider.
type OIDCProvider struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	IssuerURL             string            `json:"issuer_url"`
	ClientID              string            `json:"client_id"`
	ClientSecretEncrypted string            `json:"-"`
	ClientSecretMasked    string            `json:"client_secret,omitempty"`
	Scopes                string            `json:"scopes"`
	RoleMapping           map[string]string `json:"role_mapping"`
	DefaultRole           string            `json:"default_role"`
	AutoProvision         bool              `json:"auto_provision"`
	Enabled               bool              `json:"enabled"`
	CreatedAt             time.Time         `json:"created_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
}
