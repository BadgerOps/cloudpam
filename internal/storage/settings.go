package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// SettingsStore manages application settings.
type SettingsStore interface {
	GetSecuritySettings(ctx context.Context) (*domain.SecuritySettings, error)
	UpdateSecuritySettings(ctx context.Context, settings *domain.SecuritySettings) error
	GetNetworkSchemaPolicy(ctx context.Context) (*domain.NetworkSchemaPolicy, error)
	UpdateNetworkSchemaPolicy(ctx context.Context, policy *domain.NetworkSchemaPolicy) error
	// GetAIProviderSettings returns the stored AI provider document, or an
	// empty document when nothing is stored.
	GetAIProviderSettings(ctx context.Context) (*domain.AIProviderSettings, error)
	UpdateAIProviderSettings(ctx context.Context, settings *domain.AIProviderSettings) error
}
