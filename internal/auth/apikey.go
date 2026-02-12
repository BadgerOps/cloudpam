// Package auth provides API key authentication for CloudPAM.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	// APIKeyPrefix is the prefix for all CloudPAM API keys.
	APIKeyPrefix = "cpam_"

	// PrefixLength is the number of characters from the key used for identification.
	// This allows looking up keys without exposing the full key.
	PrefixLength = 8

	// KeyBytes is the number of random bytes used for key generation.
	KeyBytes = 32

	// Argon2id parameters (OWASP recommended for API key hashing)
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // 64MB
	argon2Threads = 4
	argon2KeyLen  = 32
)

var (
	// ErrInvalidKeyFormat indicates the API key format is invalid.
	ErrInvalidKeyFormat = errors.New("invalid API key format")

	// ErrKeyRevoked indicates the API key has been revoked.
	ErrKeyRevoked = errors.New("API key has been revoked")

	// ErrKeyExpired indicates the API key has expired.
	ErrKeyExpired = errors.New("API key has expired")

	// ErrKeyNotFound indicates the API key was not found.
	ErrKeyNotFound = errors.New("API key not found")

	// ErrInvalidKey indicates the API key failed validation.
	ErrInvalidKey = errors.New("invalid API key")

	// ErrInsufficientScopes indicates the API key lacks required permissions.
	ErrInsufficientScopes = errors.New("insufficient scopes")
)

// APIKey represents a stored API key with metadata.
type APIKey struct {
	ID         string     `json:"id"`
	Prefix     string     `json:"prefix"` // First 8 chars for identification
	Name       string     `json:"name"`   // User-provided name
	Hash       []byte     `json:"-"`      // Argon2id hash of the full key (never serialized)
	Salt       []byte     `json:"-"`      // Salt used for hashing (never serialized)
	Scopes     []string   `json:"scopes"` // Permissions: ["pools:read", "pools:write", ...]
	OwnerID    *string    `json:"owner_id,omitempty"` // nil = bot/standalone key
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"` // nil = no expiration
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Revoked    bool       `json:"revoked"`
}

// IsExpired returns true if the API key has expired.
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// IsValid returns true if the API key is active (not revoked and not expired).
func (k *APIKey) IsValid() bool {
	return !k.Revoked && !k.IsExpired()
}

// HasScope returns true if the API key has the specified scope.
func (k *APIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope || s == "*" {
			return true
		}
		// Support wildcard matching: "pools:*" matches "pools:read"
		if strings.HasSuffix(s, ":*") {
			prefix := strings.TrimSuffix(s, "*")
			if strings.HasPrefix(scope, prefix) {
				return true
			}
		}
	}
	return false
}

// HasAnyScope returns true if the API key has any of the specified scopes.
func (k *APIKey) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if k.HasScope(scope) {
			return true
		}
	}
	return false
}

// GenerateAPIKeyOptions contains options for API key generation.
type GenerateAPIKeyOptions struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

// GenerateAPIKey creates a new API key with the given options.
// It returns the plaintext key (to be shown once to the user) and the APIKey record.
// The plaintext key should never be stored; only the hash is kept.
func GenerateAPIKey(opts GenerateAPIKeyOptions) (plaintext string, apiKey *APIKey, err error) {
	// Generate random bytes for the key
	keyBytes := make([]byte, KeyBytes)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	// Encode as base64url (no padding for cleaner keys)
	encoded := base64.RawURLEncoding.EncodeToString(keyBytes)
	plaintext = APIKeyPrefix + encoded

	// Extract prefix for identification (first 8 chars after cpam_)
	prefix := encoded[:PrefixLength]

	// Generate salt for Argon2id
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Hash the key
	hash := hashKey(plaintext, salt)

	// Generate UUID for ID
	id, err := generateUUID()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate ID: %w", err)
	}

	apiKey = &APIKey{
		ID:        id,
		Prefix:    prefix,
		Name:      opts.Name,
		Hash:      hash,
		Salt:      salt,
		Scopes:    opts.Scopes,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: opts.ExpiresAt,
		Revoked:   false,
	}

	return plaintext, apiKey, nil
}

// ValidateAPIKey checks if the provided plaintext key matches the stored APIKey.
// Returns nil if valid, or an appropriate error.
func ValidateAPIKey(providedKey string, stored *APIKey) error {
	if stored == nil {
		return ErrKeyNotFound
	}

	if stored.Revoked {
		return ErrKeyRevoked
	}

	if stored.IsExpired() {
		return ErrKeyExpired
	}

	// Hash the provided key with the stored salt
	providedHash := hashKey(providedKey, stored.Salt)

	// Constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare(providedHash, stored.Hash) != 1 {
		return ErrInvalidKey
	}

	return nil
}

// ParseAPIKeyPrefix extracts the prefix from an API key for lookup.
// Returns an error if the key format is invalid.
func ParseAPIKeyPrefix(key string) (string, error) {
	if !strings.HasPrefix(key, APIKeyPrefix) {
		return "", ErrInvalidKeyFormat
	}

	keyPart := strings.TrimPrefix(key, APIKeyPrefix)
	if len(keyPart) < PrefixLength {
		return "", ErrInvalidKeyFormat
	}

	// Validate that the key contains only valid base64url characters
	if !isValidBase64URL(keyPart) {
		return "", ErrInvalidKeyFormat
	}

	return keyPart[:PrefixLength], nil
}

// hashKey hashes the API key using Argon2id.
func hashKey(key string, salt []byte) []byte {
	return argon2.IDKey([]byte(key), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
}

// isValidBase64URL checks if a string contains only valid base64url characters.
func isValidBase64URL(s string) bool {
	for _, r := range s {
		isUpper := r >= 'A' && r <= 'Z'
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		isSpecial := r == '-' || r == '_'
		if !isUpper && !isLower && !isDigit && !isSpecial {
			return false
		}
	}
	return true
}

// generateUUID generates a random UUID v4.
func generateUUID() (string, error) {
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		return "", err
	}

	// Set version (4) and variant (RFC 4122)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// MaskAPIKey returns a masked version of an API key for logging.
// Example: "cpam_abc12345..." -> "cpam_abc1****"
func MaskAPIKey(key string) string {
	if !strings.HasPrefix(key, APIKeyPrefix) {
		return "****"
	}

	keyPart := strings.TrimPrefix(key, APIKeyPrefix)
	if len(keyPart) < 4 {
		return APIKeyPrefix + "****"
	}

	return APIKeyPrefix + keyPart[:4] + "****"
}
