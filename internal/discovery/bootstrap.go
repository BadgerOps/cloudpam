package discovery

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/argon2"

	"cloudpam/internal/domain"
)

const (
	bootstrapTokenPrefix = "boot_"
	tokenLength          = 32 // bytes, becomes 43 chars in base64
)

// GenerateBootstrapToken generates a new bootstrap token with the given parameters.
func GenerateBootstrapToken(name string, accountID *int64, createdBy string, expiresIn *time.Duration, maxUses *int) (domain.BootstrapToken, error) {
	// Generate random token
	tokenBytes := make([]byte, tokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return domain.BootstrapToken{}, fmt.Errorf("generate random token: %w", err)
	}

	// Encode as base64 with prefix
	token := bootstrapTokenPrefix + base64.RawURLEncoding.EncodeToString(tokenBytes)

	// Hash the token for storage
	tokenHash := hashToken(token)

	// Calculate expiration
	var expiresAt *time.Time
	if expiresIn != nil {
		exp := time.Now().UTC().Add(*expiresIn)
		expiresAt = &exp
	}

	return domain.BootstrapToken{
		Name:      name,
		Token:     token, // return plaintext token only on creation
		TokenHash: tokenHash,
		AccountID: accountID,
		CreatedBy: createdBy,
		ExpiresAt: expiresAt,
		MaxUses:   maxUses,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ValidateBootstrapToken checks if a token string is valid and returns the token record.
func ValidateBootstrapToken(tokenString string, stored domain.BootstrapToken) bool {
	// Hash the provided token
	providedHash := hashToken(tokenString)

	// Constant-time comparison
	return subtle.ConstantTimeCompare(providedHash, stored.TokenHash) == 1
}

// hashToken uses Argon2id to hash a bootstrap token.
func hashToken(token string) []byte {
	// Use a fixed salt for bootstrap tokens (they're already random)
	// In production, you might want per-token salts, but this is simpler
	salt := []byte("cloudpam-bootstrap-token-v1")

	// Argon2id parameters (lighter than API keys since tokens are already random)
	return argon2.IDKey([]byte(token), salt, 1, 32*1024, 2, 32)
}

// ParseExpiresIn parses a duration string like "24h", "7d", "30d".
func ParseExpiresIn(s string) (*time.Duration, error) {
	if s == "" {
		return nil, nil
	}

	// Support common suffixes
	var d time.Duration
	var err error

	// Check for day suffix
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days := s[:len(s)-1]
		var n int
		_, err = fmt.Sscanf(days, "%d", &n)
		if err != nil {
			return nil, fmt.Errorf("invalid duration: %s", s)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		d, err = time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid duration: %s (use 24h, 7d, etc)", s)
		}
	}

	return &d, nil
}
