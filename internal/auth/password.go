package auth

import "golang.org/x/crypto/bcrypt"

const bcryptCost = 12

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
