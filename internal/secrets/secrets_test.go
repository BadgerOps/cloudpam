package secrets

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ct, err := Encrypt("sk-round-trip", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ct == "sk-round-trip" {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "sk-round-trip" {
		t.Fatal("round trip mismatch")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	k1, _ := GenerateKey()
	k2, _ := GenerateKey()
	ct, err := Encrypt("secret", k1)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decrypt(ct, k2); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt with wrong key err = %v, want ErrDecryptionFailed", err)
	}
}

func TestInvalidKeyLength(t *testing.T) {
	if _, err := Encrypt("x", []byte("short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Encrypt err = %v, want ErrInvalidKey", err)
	}
	if _, err := Decrypt("00", []byte("short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Decrypt err = %v, want ErrInvalidKey", err)
	}
}

func TestDecryptTruncatedCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	if _, err := Decrypt("0011", key); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt err = %v, want ErrDecryptionFailed", err)
	}
}

// Fixture produced by the original internal/auth/oidc.Encrypt before the AES-GCM
// helpers moved here (hex of nonce || sealed data). It proves stored OIDC client
// secrets keep decrypting after the move.
const (
	oidcFixtureKeyHex     = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	oidcFixturePlaintext  = "oidc-client-secret"
	oidcFixtureCiphertext = "da33d6972f32e7f7bcf28255f62140ff144c2bb342562a07d35b05a63ecabcdefce95e47807f91e1069ba1b4ea49"
)

func TestDecryptOIDCFixture(t *testing.T) {
	key, err := hex.DecodeString(oidcFixtureKeyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	got, err := Decrypt(oidcFixtureCiphertext, key)
	if err != nil {
		t.Fatalf("decrypt fixture: %v", err)
	}
	if got != oidcFixturePlaintext {
		t.Fatal("fixture plaintext mismatch")
	}
}
