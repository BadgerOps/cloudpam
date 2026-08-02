package auth

import (
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// The hashes below were produced by the golang.org/x/crypto version in use
// before the v0.52.0 upgrade. Credentials are stored, not recomputed, so a
// dependency bump that silently changed either KDF's output would lock every
// existing user and API key out of the system. These fixtures fail loudly if
// that ever happens, which a hash-then-verify round trip inside a single binary
// cannot detect.

// argon2idFixture pins IDKey output for the parameters hashKey uses.
func TestArgon2idOutputIsStableAcrossCryptoVersions(t *testing.T) {
	const (
		key          = "cpam_fixture_key_value"
		saltB64      = "Y2xvdWRwYW0tZml4ZWQtc2FsdC0wMDAwMDAwMDAwMDA"
		wantHashB64  = "69v6SdRiaDGnLRUMRh+zGGdrJe2zOXDf8PvM/WBVxjg"
		wantHashSize = argon2KeyLen
	)

	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		t.Fatalf("decode fixture salt: %v", err)
	}

	got := hashKey(key, salt)
	if len(got) != wantHashSize {
		t.Fatalf("hashKey length = %d, want %d", len(got), wantHashSize)
	}

	gotB64 := base64.RawStdEncoding.EncodeToString(got)
	if gotB64 != wantHashB64 {
		t.Errorf("argon2id output changed: got %q, want %q\n"+
			"every stored API key hash would stop verifying", gotB64, wantHashB64)
	}

	// Guard the parameters themselves, since hashKey's output is only
	// reproducible while they hold.
	direct := argon2.IDKey([]byte(key), salt, 1, 64*1024, 4, 32)
	if base64.RawStdEncoding.EncodeToString(direct) != wantHashB64 {
		t.Error("argon2id parameters drifted from time=1, memory=64MB, threads=4, keyLen=32")
	}
}

// TestBcryptVerifiesPreUpgradeHashes checks that password hashes written by the
// previous x/crypto release still authenticate.
func TestBcryptVerifiesPreUpgradeHashes(t *testing.T) {
	const (
		password = "correct-horse-battery"
		// Generated with bcrypt cost 12 before the v0.52.0 upgrade.
		storedHash = "$2a$12$iyzOrF4VVDt7ekv6Kfo/q.oDMY2Dq/lFhwqu/Pvj9YgI1RHmQiJTa"
	)

	if err := VerifyPassword(password, []byte(storedHash)); err != nil {
		t.Fatalf("VerifyPassword rejected a hash written by the previous x/crypto release: %v", err)
	}

	if err := VerifyPassword("wrong-password", []byte(storedHash)); err == nil {
		t.Error("VerifyPassword accepted an incorrect password")
	}

	cost, err := bcrypt.Cost([]byte(storedHash))
	if err != nil {
		t.Fatalf("read fixture cost: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("fixture cost = %d, want %d; regenerate the fixture if bcryptCost changed intentionally", cost, bcryptCost)
	}
}

// TestBcryptNewHashesRemainVerifiable covers the forward direction: hashes the
// upgraded library writes must be readable by the same library.
func TestBcryptNewHashesRemainVerifiable(t *testing.T) {
	hash, err := HashPassword("another-valid-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := VerifyPassword("another-valid-password", hash); err != nil {
		t.Errorf("VerifyPassword on a freshly written hash: %v", err)
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil {
		t.Fatalf("read cost: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("new hash cost = %d, want %d", cost, bcryptCost)
	}
}
