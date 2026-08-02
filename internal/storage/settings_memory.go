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

// cloneSecuritySettings deep-copies security settings so store-owned state and
// caller-owned state never share the nested slice and map fields.
func cloneSecuritySettings(in *domain.SecuritySettings) *domain.SecuritySettings {
	if in == nil {
		return nil
	}
	out := *in
	if in.TrustedProxies != nil {
		out.TrustedProxies = append([]string(nil), in.TrustedProxies...)
	}
	if in.APIKeyAllowedScopesByRole != nil {
		out.APIKeyAllowedScopesByRole = make(map[string][]string, len(in.APIKeyAllowedScopesByRole))
		for role, scopes := range in.APIKeyAllowedScopesByRole {
			if scopes == nil {
				out.APIKeyAllowedScopesByRole[role] = nil
				continue
			}
			out.APIKeyAllowedScopesByRole[role] = append([]string(nil), scopes...)
		}
	}
	return &out
}

// NewMemorySettingsStore creates a new in-memory settings store with defaults.
func NewMemorySettingsStore() *MemorySettingsStore {
	defaults := domain.DefaultSecuritySettings()
	policy := domain.DefaultNetworkSchemaPolicy()
	return &MemorySettingsStore{security: cloneSecuritySettings(&defaults), networkSchemaPolicy: &policy}
}

func (s *MemorySettingsStore) GetSecuritySettings(_ context.Context) (*domain.SecuritySettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return domain.NormalizeSecuritySettings(cloneSecuritySettings(s.security)), nil
}

func (s *MemorySettingsStore) UpdateSecuritySettings(_ context.Context, settings *domain.SecuritySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.security = domain.NormalizeSecuritySettings(cloneSecuritySettings(settings))
	return nil
}

func (s *MemorySettingsStore) GetNetworkSchemaPolicy(_ context.Context) (*domain.NetworkSchemaPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dup := *s.networkSchemaPolicy
	return domain.NormalizeNetworkSchemaPolicy(&dup), nil
}

func (s *MemorySettingsStore) UpdateNetworkSchemaPolicy(_ context.Context, policy *domain.NetworkSchemaPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networkSchemaPolicy = domain.NormalizeNetworkSchemaPolicy(policy)
	return nil
}
