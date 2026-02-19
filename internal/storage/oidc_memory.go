package storage

import (
	"context"
	"sync"

	"cloudpam/internal/domain"
)

// MemoryOIDCProviderStore is an in-memory implementation of OIDCProviderStore.
type MemoryOIDCProviderStore struct {
	mu          sync.RWMutex
	providers   map[string]*domain.OIDCProvider // keyed by ID
	issuerIndex map[string]string               // issuer URL -> ID
}

// NewMemoryOIDCProviderStore creates a new in-memory OIDC provider store.
func NewMemoryOIDCProviderStore() *MemoryOIDCProviderStore {
	return &MemoryOIDCProviderStore{
		providers:   make(map[string]*domain.OIDCProvider),
		issuerIndex: make(map[string]string),
	}
}

func (s *MemoryOIDCProviderStore) CreateProvider(_ context.Context, p *domain.OIDCProvider) error {
	if p == nil || p.ID == "" {
		return ErrValidation
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.providers[p.ID]; exists {
		return ErrConflict
	}
	if _, exists := s.issuerIndex[p.IssuerURL]; exists {
		return ErrDuplicateIssuer
	}

	s.providers[p.ID] = copyOIDCProvider(p)
	s.issuerIndex[p.IssuerURL] = p.ID
	return nil
}

func (s *MemoryOIDCProviderStore) GetProvider(_ context.Context, id string) (*domain.OIDCProvider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, exists := s.providers[id]
	if !exists {
		return nil, ErrNotFound
	}
	return copyOIDCProvider(p), nil
}

func (s *MemoryOIDCProviderStore) GetProviderByIssuer(_ context.Context, issuerURL string) (*domain.OIDCProvider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, exists := s.issuerIndex[issuerURL]
	if !exists {
		return nil, ErrNotFound
	}
	return copyOIDCProvider(s.providers[id]), nil
}

func (s *MemoryOIDCProviderStore) ListProviders(_ context.Context) ([]*domain.OIDCProvider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*domain.OIDCProvider, 0, len(s.providers))
	for _, p := range s.providers {
		result = append(result, copyOIDCProvider(p))
	}
	return result, nil
}

func (s *MemoryOIDCProviderStore) ListEnabledProviders(_ context.Context) ([]*domain.OIDCProvider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*domain.OIDCProvider
	for _, p := range s.providers {
		if p.Enabled {
			result = append(result, copyOIDCProvider(p))
		}
	}
	return result, nil
}

func (s *MemoryOIDCProviderStore) UpdateProvider(_ context.Context, p *domain.OIDCProvider) error {
	if p == nil || p.ID == "" {
		return ErrValidation
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.providers[p.ID]
	if !exists {
		return ErrNotFound
	}

	// If issuer URL changed, update index and check for duplicates.
	if existing.IssuerURL != p.IssuerURL {
		if _, taken := s.issuerIndex[p.IssuerURL]; taken {
			return ErrDuplicateIssuer
		}
		delete(s.issuerIndex, existing.IssuerURL)
		s.issuerIndex[p.IssuerURL] = p.ID
	}

	s.providers[p.ID] = copyOIDCProvider(p)
	return nil
}

func (s *MemoryOIDCProviderStore) DeleteProvider(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.providers[id]
	if !exists {
		return ErrNotFound
	}

	delete(s.issuerIndex, p.IssuerURL)
	delete(s.providers, id)
	return nil
}

// copyOIDCProvider creates a deep copy of an OIDCProvider.
func copyOIDCProvider(p *domain.OIDCProvider) *domain.OIDCProvider {
	if p == nil {
		return nil
	}
	cpy := *p
	if p.RoleMapping != nil {
		cpy.RoleMapping = make(map[string]string, len(p.RoleMapping))
		for k, v := range p.RoleMapping {
			cpy.RoleMapping[k] = v
		}
	}
	return &cpy
}
