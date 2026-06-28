package storage

import (
	"context"
	"sync"

	"cloudpam/internal/domain"
)

// MemorySettingsStore is an in-memory implementation of SettingsStore.
type MemorySettingsStore struct {
	mu                  sync.RWMutex
	security            *domain.SecuritySettings
	networkSchemaPolicy *domain.NetworkSchemaPolicy
}

// NewMemorySettingsStore creates a new in-memory settings store with defaults.
func NewMemorySettingsStore() *MemorySettingsStore {
	defaults := domain.DefaultSecuritySettings()
	policy := domain.DefaultNetworkSchemaPolicy()
	return &MemorySettingsStore{security: &defaults, networkSchemaPolicy: &policy}
}

func (s *MemorySettingsStore) GetSecuritySettings(_ context.Context) (*domain.SecuritySettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := *s.security
	return domain.NormalizeSecuritySettings(&copy), nil
}

func (s *MemorySettingsStore) UpdateSecuritySettings(_ context.Context, settings *domain.SecuritySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.security = domain.NormalizeSecuritySettings(settings)
	return nil
}

func (s *MemorySettingsStore) GetNetworkSchemaPolicy(_ context.Context) (*domain.NetworkSchemaPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := *s.networkSchemaPolicy
	return domain.NormalizeNetworkSchemaPolicy(&copy), nil
}

func (s *MemorySettingsStore) UpdateNetworkSchemaPolicy(_ context.Context, policy *domain.NetworkSchemaPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networkSchemaPolicy = domain.NormalizeNetworkSchemaPolicy(policy)
	return nil
}
