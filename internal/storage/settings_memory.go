package storage

import (
	"context"
	"sync"

	"cloudpam/internal/domain"
)

// MemorySettingsStore is an in-memory implementation of SettingsStore.
type MemorySettingsStore struct {
	mu       sync.RWMutex
	security *domain.SecuritySettings
}

// NewMemorySettingsStore creates a new in-memory settings store with defaults.
func NewMemorySettingsStore() *MemorySettingsStore {
	defaults := domain.DefaultSecuritySettings()
	return &MemorySettingsStore{security: &defaults}
}

func (s *MemorySettingsStore) GetSecuritySettings(_ context.Context) (*domain.SecuritySettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := *s.security
	return &copy, nil
}

func (s *MemorySettingsStore) UpdateSecuritySettings(_ context.Context, settings *domain.SecuritySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.security = settings
	return nil
}
