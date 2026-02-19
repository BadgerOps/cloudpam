package auth

import (
	"errors"
	"time"
)

// User errors.
var (
	ErrUserNotFound    = errors.New("user not found")
	ErrUserExists      = errors.New("user already exists")
	ErrInvalidPassword = errors.New("invalid password")
	ErrUserDisabled    = errors.New("user account is disabled")
)

// User represents a local user account.
type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email,omitempty"`
	DisplayName  string     `json:"display_name,omitempty"`
	Role         Role       `json:"role"`
	PasswordHash []byte     `json:"-"` // bcrypt hash, never serialized
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	AuthProvider string     `json:"auth_provider,omitempty"` // "local" or "oidc"
	OIDCSubject  string     `json:"oidc_subject,omitempty"`  // IdP "sub" claim
	OIDCIssuer   string     `json:"oidc_issuer,omitempty"`   // IdP issuer URL
}

// copyUser creates a deep copy of a User.
func copyUser(u *User) *User {
	if u == nil {
		return nil
	}
	cpy := &User{
		ID:           u.ID,
		Username:     u.Username,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		Role:         u.Role,
		IsActive:     u.IsActive,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
		AuthProvider: u.AuthProvider,
		OIDCSubject:  u.OIDCSubject,
		OIDCIssuer:   u.OIDCIssuer,
	}
	if u.PasswordHash != nil {
		cpy.PasswordHash = make([]byte, len(u.PasswordHash))
		copy(cpy.PasswordHash, u.PasswordHash)
	}
	if u.LastLoginAt != nil {
		t := *u.LastLoginAt
		cpy.LastLoginAt = &t
	}
	return cpy
}
