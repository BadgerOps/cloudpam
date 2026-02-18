package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// OIDCProviderStore defines the interface for OIDC provider persistence.
type OIDCProviderStore interface {
	// CreateProvider stores a new OIDC provider configuration.
	CreateProvider(ctx context.Context, p *domain.OIDCProvider) error

	// GetProvider retrieves an OIDC provider by ID.
	GetProvider(ctx context.Context, id string) (*domain.OIDCProvider, error)

	// GetProviderByIssuer retrieves an OIDC provider by issuer URL.
	GetProviderByIssuer(ctx context.Context, issuerURL string) (*domain.OIDCProvider, error)

	// ListProviders returns all configured OIDC providers.
	ListProviders(ctx context.Context) ([]*domain.OIDCProvider, error)

	// ListEnabledProviders returns only enabled OIDC providers.
	ListEnabledProviders(ctx context.Context) ([]*domain.OIDCProvider, error)

	// UpdateProvider modifies an existing OIDC provider configuration.
	UpdateProvider(ctx context.Context, p *domain.OIDCProvider) error

	// DeleteProvider removes an OIDC provider by ID.
	DeleteProvider(ctx context.Context, id string) error
}
