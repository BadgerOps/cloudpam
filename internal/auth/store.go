package auth

import (
	"context"
	"sync"
	"time"
)

// KeyStore defines the interface for API key storage operations.
type KeyStore interface {
	// Create stores a new API key.
	Create(ctx context.Context, key *APIKey) error

	// GetByPrefix retrieves an API key by its prefix.
	// Returns nil, nil if not found.
	GetByPrefix(ctx context.Context, prefix string) (*APIKey, error)

	// GetByID retrieves an API key by its ID.
	// Returns nil, nil if not found.
	GetByID(ctx context.Context, id string) (*APIKey, error)

	// List returns all API keys (without sensitive data).
	List(ctx context.Context) ([]*APIKey, error)

	// Revoke marks an API key as revoked.
	Revoke(ctx context.Context, id string) error

	// UpdateLastUsed updates the last used timestamp for an API key.
	UpdateLastUsed(ctx context.Context, id string, t time.Time) error

	// Delete permanently removes an API key.
	Delete(ctx context.Context, id string) error
}

// MemoryKeyStore is an in-memory implementation of KeyStore.
// It is thread-safe and suitable for development and testing.
type MemoryKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*APIKey // keyed by ID

	// prefixIndex maps prefix -> ID for fast lookup
	prefixIndex map[string]string
}

// NewMemoryKeyStore creates a new in-memory key store.
func NewMemoryKeyStore() *MemoryKeyStore {
	return &MemoryKeyStore{
		keys:        make(map[string]*APIKey),
		prefixIndex: make(map[string]string),
	}
}

// Create stores a new API key.
func (s *MemoryKeyStore) Create(_ context.Context, key *APIKey) error {
	if key == nil {
		return ErrKeyNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate prefix
	if _, exists := s.prefixIndex[key.Prefix]; exists {
		// Prefix collision is extremely unlikely with 8 chars of base64url,
		// but we handle it gracefully
		return ErrInvalidKeyFormat
	}

	// Store a copy to prevent external mutation
	stored := copyAPIKey(key)
	s.keys[key.ID] = stored
	s.prefixIndex[key.Prefix] = key.ID

	return nil
}

// GetByPrefix retrieves an API key by its prefix.
func (s *MemoryKeyStore) GetByPrefix(_ context.Context, prefix string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, exists := s.prefixIndex[prefix]
	if !exists {
		return nil, nil
	}

	key, exists := s.keys[id]
	if !exists {
		return nil, nil
	}

	return copyAPIKey(key), nil
}

// GetByID retrieves an API key by its ID.
func (s *MemoryKeyStore) GetByID(_ context.Context, id string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, exists := s.keys[id]
	if !exists {
		return nil, nil
	}

	return copyAPIKey(key), nil
}

// List returns all API keys.
func (s *MemoryKeyStore) List(_ context.Context) ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		// Return copy without hash/salt for security
		k := &APIKey{
			ID:         key.ID,
			Prefix:     key.Prefix,
			Name:       key.Name,
			Scopes:     append([]string(nil), key.Scopes...),
			CreatedAt:  key.CreatedAt,
			ExpiresAt:  key.ExpiresAt,
			LastUsedAt: key.LastUsedAt,
			Revoked:    key.Revoked,
		}
		result = append(result, k)
	}

	return result, nil
}

// Revoke marks an API key as revoked.
func (s *MemoryKeyStore) Revoke(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return ErrKeyNotFound
	}

	key.Revoked = true
	return nil
}

// UpdateLastUsed updates the last used timestamp for an API key.
func (s *MemoryKeyStore) UpdateLastUsed(_ context.Context, id string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return ErrKeyNotFound
	}

	key.LastUsedAt = &t
	return nil
}

// Delete permanently removes an API key.
func (s *MemoryKeyStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return ErrKeyNotFound
	}

	delete(s.prefixIndex, key.Prefix)
	delete(s.keys, id)
	return nil
}

// copyAPIKey creates a deep copy of an APIKey.
func copyAPIKey(key *APIKey) *APIKey {
	if key == nil {
		return nil
	}

	copy := &APIKey{
		ID:        key.ID,
		Prefix:    key.Prefix,
		Name:      key.Name,
		CreatedAt: key.CreatedAt,
		Revoked:   key.Revoked,
	}

	if key.Hash != nil {
		copy.Hash = make([]byte, len(key.Hash))
		copyBytes(copy.Hash, key.Hash)
	}

	if key.Salt != nil {
		copy.Salt = make([]byte, len(key.Salt))
		copyBytes(copy.Salt, key.Salt)
	}

	if key.Scopes != nil {
		copy.Scopes = make([]string, len(key.Scopes))
		copyStrings(copy.Scopes, key.Scopes)
	}

	if key.ExpiresAt != nil {
		t := *key.ExpiresAt
		copy.ExpiresAt = &t
	}

	if key.LastUsedAt != nil {
		t := *key.LastUsedAt
		copy.LastUsedAt = &t
	}

	return copy
}

func copyBytes(dst, src []byte) {
	for i := range src {
		dst[i] = src[i]
	}
}

func copyStrings(dst, src []string) {
	for i := range src {
		dst[i] = src[i]
	}
}
