package oidc

import "cloudpam/internal/secrets"

// These wrappers keep the OIDC API stable; the AES-256-GCM implementation and
// its ciphertext format live in internal/secrets.
var (
	ErrInvalidKey       = secrets.ErrInvalidKey
	ErrDecryptionFailed = secrets.ErrDecryptionFailed
)

// Encrypt encrypts plaintext using AES-256-GCM with the given 32-byte key.
func Encrypt(plaintext string, key []byte) (string, error) { return secrets.Encrypt(plaintext, key) }

// Decrypt decrypts hex-encoded AES-256-GCM ciphertext with the given 32-byte key.
func Decrypt(ciphertextHex string, key []byte) (string, error) {
	return secrets.Decrypt(ciphertextHex, key)
}

// GenerateEncryptionKey generates a random 32-byte AES-256 key.
func GenerateEncryptionKey() ([]byte, error) { return secrets.GenerateKey() }
