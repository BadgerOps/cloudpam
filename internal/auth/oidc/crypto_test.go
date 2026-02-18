package oidc

import (
	"encoding/hex"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintext := "my-client-secret"
	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if got != plaintext {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncrypt_DifferentCiphertexts(t *testing.T) {
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintext := "same-secret"
	ct1, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt 1: %v", err)
	}
	ct2, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt 2: %v", err)
	}

	if ct1 == ct2 {
		t.Error("expected different ciphertexts due to random nonce, got identical")
	}

	// Both should still decrypt to same plaintext.
	for i, ct := range []string{ct1, ct2} {
		got, err := Decrypt(ct, key)
		if err != nil {
			t.Fatalf("decrypt %d: %v", i+1, err)
		}
		if got != plaintext {
			t.Errorf("decrypt %d: got %q, want %q", i+1, got, plaintext)
		}
	}
}

func TestDecrypt_InvalidKey(t *testing.T) {
	keyA, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	keyB, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	ciphertext, err := Encrypt("secret-data", keyA)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = Decrypt(ciphertext, keyB)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
	if err != ErrDecryptionFailed {
		t.Errorf("expected ErrDecryptionFailed, got: %v", err)
	}
}

func TestDecrypt_CorruptedData(t *testing.T) {
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Hex-encoded garbage that is long enough to have a nonce prefix.
	garbage := hex.EncodeToString(make([]byte, 64))
	_, err = Decrypt(garbage, key)
	if err == nil {
		t.Fatal("expected error decrypting corrupted data")
	}
}

func TestDecrypt_InvalidHex(t *testing.T) {
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	_, err = Decrypt("not-valid-hex!!!", key)
	if err == nil {
		t.Fatal("expected error for invalid hex input")
	}
}

func TestGenerateEncryptionKey(t *testing.T) {
	k1, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key 1: %v", err)
	}
	if len(k1) != 32 {
		t.Errorf("key length: got %d, want 32", len(k1))
	}

	k2, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate key 2: %v", err)
	}
	if len(k2) != 32 {
		t.Errorf("key length: got %d, want 32", len(k2))
	}

	if hex.EncodeToString(k1) == hex.EncodeToString(k2) {
		t.Error("expected two generated keys to differ")
	}
}

func TestEncrypt_InvalidKeyLength(t *testing.T) {
	for _, keyLen := range []int{0, 16, 24, 31, 33, 64} {
		key := make([]byte, keyLen)
		_, err := Encrypt("test", key)
		if err != ErrInvalidKey {
			t.Errorf("key length %d: expected ErrInvalidKey, got: %v", keyLen, err)
		}
	}
}

func TestDecrypt_InvalidKeyLength(t *testing.T) {
	// Also verify Decrypt rejects bad key lengths.
	_, err := Decrypt("aabbccdd", make([]byte, 16))
	if err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got: %v", err)
	}
}
