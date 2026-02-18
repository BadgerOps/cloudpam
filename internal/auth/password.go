package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 12

	// DefaultMinPasswordLength is the minimum password length enforced by default.
	DefaultMinPasswordLength = 12

	// MaxPasswordLength is the maximum password length (bcrypt truncation boundary).
	MaxPasswordLength = 72
)

// ValidatePassword checks password meets policy requirements.
func ValidatePassword(password string, minLength int) error {
	if minLength <= 0 {
		minLength = DefaultMinPasswordLength
	}
	if len(password) < minLength {
		return fmt.Errorf("password must be at least %d characters", minLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("password must be at most %d characters", MaxPasswordLength)
	}
	return nil
}

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
}

// VerifyPassword checks a plaintext password against a bcrypt hash.
// Returns ErrInvalidPassword if the password does not match.
func VerifyPassword(password string, hash []byte) error {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err != nil {
		return ErrInvalidPassword
	}
	return nil
}
