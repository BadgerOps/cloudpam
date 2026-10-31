// Package secrets holds the application's at-rest secret encryption: AES-256-GCM
// with a 32-byte key, hex-encoded as nonce || ciphertext. It protects OIDC
// client secrets and AI provider API keys with the key from
// CLOUDPAM_OIDC_ENCRYPTION_KEY.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrInvalidKey reports a key that is not 32 bytes.
	ErrInvalidKey = errors.New("encryption key must be 32 bytes")
	// ErrDecryptionFailed reports ciphertext that does not open under the key.
	ErrDecryptionFailed = errors.New("decryption failed")
)

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return gcm, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with the given 32-byte key.
// Returns hex-encoded ciphertext (nonce + encrypted data).
func Encrypt(plaintext string, key []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// Decrypt decrypts hex-encoded AES-256-GCM ciphertext with the given 32-byte key.
func Decrypt(ciphertextHex string, key []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", fmt.Errorf("decode hex: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", ErrDecryptionFailed
	}
	nonce, data := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}
	return string(plaintext), nil
}

// GenerateKey generates a random 32-byte AES-256 key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return key, nil
}
