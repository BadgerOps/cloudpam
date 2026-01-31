package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGenerateAPIKey(t *testing.T) {
	opts := GenerateAPIKeyOptions{
		Name:   "Test Key",
		Scopes: []string{"pools:read", "accounts:read"},
	}

	plaintext, apiKey, err := GenerateAPIKey(opts)
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}

	// Check plaintext format
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		t.Errorf("plaintext should start with %q, got %q", APIKeyPrefix, plaintext)
	}

	// Key should be ~48 chars: cpam_ (5) + base64url(32 bytes) (43)
	if len(plaintext) < 40 || len(plaintext) > 60 {
		t.Errorf("unexpected plaintext length: %d", len(plaintext))
	}

	// Check APIKey fields
	if apiKey.ID == "" {
		t.Error("ID should not be empty")
	}
	if len(apiKey.Prefix) != PrefixLength {
		t.Errorf("prefix length should be %d, got %d", PrefixLength, len(apiKey.Prefix))
	}
	if apiKey.Name != opts.Name {
		t.Errorf("name should be %q, got %q", opts.Name, apiKey.Name)
	}
	if len(apiKey.Hash) != argon2KeyLen {
		t.Errorf("hash length should be %d, got %d", argon2KeyLen, len(apiKey.Hash))
	}
	if len(apiKey.Salt) != 16 {
		t.Errorf("salt length should be 16, got %d", len(apiKey.Salt))
	}
	if len(apiKey.Scopes) != 2 {
		t.Errorf("scopes length should be 2, got %d", len(apiKey.Scopes))
	}
	if apiKey.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if apiKey.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil when not set")
	}
	if apiKey.Revoked {
		t.Error("Revoked should be false")
	}
}

func TestGenerateAPIKeyWithExpiration(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour)
	opts := GenerateAPIKeyOptions{
		Name:      "Expiring Key",
		Scopes:    []string{"pools:read"},
		ExpiresAt: &expiresAt,
	}

	_, apiKey, err := GenerateAPIKey(opts)
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}

	if apiKey.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
	if !apiKey.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt mismatch: got %v, want %v", apiKey.ExpiresAt, expiresAt)
	}
}

func TestValidateAPIKey(t *testing.T) {
	opts := GenerateAPIKeyOptions{
		Name:   "Test Key",
		Scopes: []string{"pools:read"},
	}

	plaintext, apiKey, err := GenerateAPIKey(opts)
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}

	// Valid key should pass
	if err := ValidateAPIKey(plaintext, apiKey); err != nil {
		t.Errorf("ValidateAPIKey should succeed for valid key: %v", err)
	}

	// Wrong key should fail
	if err := ValidateAPIKey("cpam_wrongkey123456789012345678901234567890", apiKey); err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey for wrong key, got %v", err)
	}

	// Nil stored key should fail
	if err := ValidateAPIKey(plaintext, nil); err != ErrKeyNotFound {
		t.Errorf("expected ErrKeyNotFound for nil stored key, got %v", err)
	}
}

func TestValidateAPIKeyRevoked(t *testing.T) {
	opts := GenerateAPIKeyOptions{
		Name:   "Test Key",
		Scopes: []string{"pools:read"},
	}

	plaintext, apiKey, _ := GenerateAPIKey(opts)
	apiKey.Revoked = true

	if err := ValidateAPIKey(plaintext, apiKey); err != ErrKeyRevoked {
		t.Errorf("expected ErrKeyRevoked, got %v", err)
	}
}

func TestValidateAPIKeyExpired(t *testing.T) {
	expired := time.Now().Add(-1 * time.Hour)
	opts := GenerateAPIKeyOptions{
		Name:      "Test Key",
		Scopes:    []string{"pools:read"},
		ExpiresAt: &expired,
	}

	plaintext, apiKey, _ := GenerateAPIKey(opts)

	if err := ValidateAPIKey(plaintext, apiKey); err != ErrKeyExpired {
		t.Errorf("expected ErrKeyExpired, got %v", err)
	}
}

func TestParseAPIKeyPrefix(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		want    string
		wantErr error
	}{
		{
			name:    "valid key",
			key:     "cpam_abcdefgh12345678901234567890123456789012",
			want:    "abcdefgh",
			wantErr: nil,
		},
		{
			name:    "missing prefix",
			key:     "abcdefgh12345678901234567890123456789012",
			want:    "",
			wantErr: ErrInvalidKeyFormat,
		},
		{
			name:    "wrong prefix",
			key:     "xyz_abcdefgh12345678901234567890123456789012",
			want:    "",
			wantErr: ErrInvalidKeyFormat,
		},
		{
			name:    "too short",
			key:     "cpam_abc",
			want:    "",
			wantErr: ErrInvalidKeyFormat,
		},
		{
			name:    "invalid characters",
			key:     "cpam_abc!@#$%^&*",
			want:    "",
			wantErr: ErrInvalidKeyFormat,
		},
		{
			name:    "empty",
			key:     "",
			want:    "",
			wantErr: ErrInvalidKeyFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAPIKeyPrefix(tt.key)
			if err != tt.wantErr {
				t.Errorf("ParseAPIKeyPrefix() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseAPIKeyPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKeyIsExpired(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	tests := []struct {
		name      string
		expiresAt *time.Time
		want      bool
	}{
		{"no expiration", nil, false},
		{"expired", &past, true},
		{"not expired", &future, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &APIKey{ExpiresAt: tt.expiresAt}
			if got := key.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKeyIsValid(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	tests := []struct {
		name      string
		revoked   bool
		expiresAt *time.Time
		want      bool
	}{
		{"active key", false, nil, true},
		{"revoked key", true, nil, false},
		{"expired key", false, &past, false},
		{"active with future expiry", false, &future, true},
		{"revoked and expired", true, &past, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &APIKey{Revoked: tt.revoked, ExpiresAt: tt.expiresAt}
			if got := key.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKeyHasScope(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		scope  string
		want   bool
	}{
		{"exact match", []string{"pools:read"}, "pools:read", true},
		{"no match", []string{"pools:read"}, "pools:write", false},
		{"wildcard all", []string{"*"}, "anything", true},
		{"wildcard resource", []string{"pools:*"}, "pools:read", true},
		{"wildcard resource", []string{"pools:*"}, "pools:write", true},
		{"wildcard resource no match", []string{"pools:*"}, "accounts:read", false},
		{"multiple scopes", []string{"pools:read", "accounts:write"}, "accounts:write", true},
		{"empty scopes", []string{}, "pools:read", false},
		{"nil scopes", nil, "pools:read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &APIKey{Scopes: tt.scopes}
			if got := key.HasScope(tt.scope); got != tt.want {
				t.Errorf("HasScope(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestAPIKeyHasAnyScope(t *testing.T) {
	key := &APIKey{Scopes: []string{"pools:read", "accounts:read"}}

	tests := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"one match", []string{"pools:read"}, true},
		{"multiple matches", []string{"pools:read", "accounts:read"}, true},
		{"one of many", []string{"pools:write", "accounts:read"}, true},
		{"no match", []string{"pools:write", "accounts:write"}, false},
		{"empty", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := key.HasAnyScope(tt.scopes...); got != tt.want {
				t.Errorf("HasAnyScope(%v) = %v, want %v", tt.scopes, got, tt.want)
			}
		})
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"valid key", "cpam_abcdefgh12345678901234567890123456789012", "cpam_abcd****"},
		{"short key", "cpam_abc", "cpam_****"},
		{"wrong prefix", "xyz_abcdefgh", "****"},
		{"empty", "", "****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskAPIKey(tt.key); got != tt.want {
				t.Errorf("MaskAPIKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestMemoryKeyStore(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()

	// Generate a key
	plaintext, apiKey, err := GenerateAPIKey(GenerateAPIKeyOptions{
		Name:   "Test Key",
		Scopes: []string{"pools:read"},
	})
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}

	// Create
	if err := store.Create(ctx, apiKey); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// GetByPrefix
	retrieved, err := store.GetByPrefix(ctx, apiKey.Prefix)
	if err != nil {
		t.Fatalf("GetByPrefix failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetByPrefix returned nil")
	}
	if retrieved.ID != apiKey.ID {
		t.Errorf("ID mismatch: got %q, want %q", retrieved.ID, apiKey.ID)
	}

	// Validate retrieved key works
	if err := ValidateAPIKey(plaintext, retrieved); err != nil {
		t.Errorf("ValidateAPIKey failed with retrieved key: %v", err)
	}

	// GetByID
	byID, err := store.GetByID(ctx, apiKey.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if byID == nil {
		t.Fatal("GetByID returned nil")
	}

	// List
	keys, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("List returned %d keys, want 1", len(keys))
	}
	// List should not include hash/salt
	if keys[0].Hash != nil {
		t.Error("List should not include hash")
	}
	if keys[0].Salt != nil {
		t.Error("List should not include salt")
	}

	// UpdateLastUsed
	now := time.Now()
	if err := store.UpdateLastUsed(ctx, apiKey.ID, now); err != nil {
		t.Fatalf("UpdateLastUsed failed: %v", err)
	}
	updated, _ := store.GetByID(ctx, apiKey.ID)
	if updated.LastUsedAt == nil || !updated.LastUsedAt.Equal(now) {
		t.Error("LastUsedAt not updated correctly")
	}

	// Revoke
	if err := store.Revoke(ctx, apiKey.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}
	revoked, _ := store.GetByID(ctx, apiKey.ID)
	if !revoked.Revoked {
		t.Error("key should be revoked")
	}

	// Delete
	if err := store.Delete(ctx, apiKey.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	deleted, _ := store.GetByID(ctx, apiKey.ID)
	if deleted != nil {
		t.Error("key should be deleted")
	}
	deletedByPrefix, _ := store.GetByPrefix(ctx, apiKey.Prefix)
	if deletedByPrefix != nil {
		t.Error("prefix index should be cleared")
	}
}

func TestMemoryKeyStoreNotFound(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()

	// GetByPrefix not found
	key, err := store.GetByPrefix(ctx, "notfound")
	if err != nil {
		t.Errorf("GetByPrefix should not error for not found: %v", err)
	}
	if key != nil {
		t.Error("GetByPrefix should return nil for not found")
	}

	// GetByID not found
	key, err = store.GetByID(ctx, "notfound")
	if err != nil {
		t.Errorf("GetByID should not error for not found: %v", err)
	}
	if key != nil {
		t.Error("GetByID should return nil for not found")
	}

	// Revoke not found
	if err := store.Revoke(ctx, "notfound"); err != ErrKeyNotFound {
		t.Errorf("Revoke should return ErrKeyNotFound, got %v", err)
	}

	// UpdateLastUsed not found
	if err := store.UpdateLastUsed(ctx, "notfound", time.Now()); err != ErrKeyNotFound {
		t.Errorf("UpdateLastUsed should return ErrKeyNotFound, got %v", err)
	}

	// Delete not found
	if err := store.Delete(ctx, "notfound"); err != ErrKeyNotFound {
		t.Errorf("Delete should return ErrKeyNotFound, got %v", err)
	}
}

func TestMemoryKeyStoreCreateNil(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()

	if err := store.Create(ctx, nil); err != ErrKeyNotFound {
		t.Errorf("Create(nil) should return ErrKeyNotFound, got %v", err)
	}
}

func TestMemoryKeyStoreConcurrency(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()

	// Create multiple keys concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, apiKey, _ := GenerateAPIKey(GenerateAPIKeyOptions{
				Name:   "Concurrent Key",
				Scopes: []string{"pools:read"},
			})
			_ = store.Create(ctx, apiKey)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	keys, _ := store.List(ctx)
	if len(keys) != 10 {
		t.Errorf("expected 10 keys, got %d", len(keys))
	}
}

func TestContextWithAPIKey(t *testing.T) {
	ctx := context.Background()

	// Empty context
	if key := APIKeyFromContext(ctx); key != nil {
		t.Error("empty context should return nil")
	}

	// With key
	apiKey := &APIKey{ID: "test-id", Name: "Test"}
	ctx = ContextWithAPIKey(ctx, apiKey)

	retrieved := APIKeyFromContext(ctx)
	if retrieved == nil {
		t.Fatal("expected key in context")
	}
	if retrieved.ID != apiKey.ID {
		t.Errorf("ID mismatch: got %q, want %q", retrieved.ID, apiKey.ID)
	}

	// Nil key
	ctx2 := ContextWithAPIKey(context.Background(), nil)
	if key := APIKeyFromContext(ctx2); key != nil {
		t.Error("nil key should not be stored")
	}

	// Nil context
	if key := APIKeyFromContext(nil); key != nil { //nolint:staticcheck // testing nil context handling
		t.Error("nil context should return nil")
	}
}

func TestIsAuthenticated(t *testing.T) {
	tests := []struct {
		name string
		key  *APIKey
		want bool
	}{
		{"valid key", &APIKey{ID: "test", Revoked: false}, true},
		{"revoked key", &APIKey{ID: "test", Revoked: true}, false},
		{"nil key", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.key != nil {
				ctx = ContextWithAPIKey(ctx, tt.key)
			}
			if got := IsAuthenticated(ctx); got != tt.want {
				t.Errorf("IsAuthenticated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequireScope(t *testing.T) {
	tests := []struct {
		name    string
		key     *APIKey
		scope   string
		wantErr error
	}{
		{
			name:    "has scope",
			key:     &APIKey{Scopes: []string{"pools:read"}},
			scope:   "pools:read",
			wantErr: nil,
		},
		{
			name:    "missing scope",
			key:     &APIKey{Scopes: []string{"pools:read"}},
			scope:   "pools:write",
			wantErr: ErrInsufficientScopes,
		},
		{
			name:    "nil key",
			key:     nil,
			scope:   "pools:read",
			wantErr: ErrKeyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.key != nil {
				ctx = ContextWithAPIKey(ctx, tt.key)
			}
			if err := RequireScope(ctx, tt.scope); err != tt.wantErr {
				t.Errorf("RequireScope() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireAnyScope(t *testing.T) {
	tests := []struct {
		name    string
		key     *APIKey
		scopes  []string
		wantErr error
	}{
		{
			name:    "has one scope",
			key:     &APIKey{Scopes: []string{"pools:read"}},
			scopes:  []string{"pools:read", "pools:write"},
			wantErr: nil,
		},
		{
			name:    "missing all scopes",
			key:     &APIKey{Scopes: []string{"accounts:read"}},
			scopes:  []string{"pools:read", "pools:write"},
			wantErr: ErrInsufficientScopes,
		},
		{
			name:    "nil key",
			key:     nil,
			scopes:  []string{"pools:read"},
			wantErr: ErrKeyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.key != nil {
				ctx = ContextWithAPIKey(ctx, tt.key)
			}
			if err := RequireAnyScope(ctx, tt.scopes...); err != tt.wantErr {
				t.Errorf("RequireAnyScope() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGeneratedKeysAreUnique(t *testing.T) {
	keys := make(map[string]bool)
	prefixes := make(map[string]bool)

	for i := 0; i < 100; i++ {
		plaintext, apiKey, err := GenerateAPIKey(GenerateAPIKeyOptions{
			Name:   "Test",
			Scopes: []string{"pools:read"},
		})
		if err != nil {
			t.Fatalf("GenerateAPIKey failed: %v", err)
		}

		if keys[plaintext] {
			t.Error("duplicate plaintext key generated")
		}
		keys[plaintext] = true

		if prefixes[apiKey.Prefix] {
			t.Error("duplicate prefix generated")
		}
		prefixes[apiKey.Prefix] = true
	}
}
