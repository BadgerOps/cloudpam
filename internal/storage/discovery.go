package storage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// DiscoveryStore provides storage operations for cloud discovery resources and sync jobs.
type DiscoveryStore interface {
	// ListDiscoveredResources returns paginated discovered resources for an account.
	ListDiscoveredResources(ctx context.Context, accountID int64, filters domain.DiscoveryFilters) ([]domain.DiscoveredResource, int, error)

	// GetDiscoveredResource returns a single discovered resource by ID.
	GetDiscoveredResource(ctx context.Context, id uuid.UUID) (*domain.DiscoveredResource, error)

	// UpsertDiscoveredResource inserts or updates a discovered resource, keyed by (account_id, resource_id).
	UpsertDiscoveredResource(ctx context.Context, res domain.DiscoveredResource) error

	// MarkStaleResources marks resources not seen since the given time as stale. Returns count affected.
	MarkStaleResources(ctx context.Context, accountID int64, before time.Time) (int, error)

	// LinkResourceToPool links a discovered resource to a managed pool.
	LinkResourceToPool(ctx context.Context, resourceID uuid.UUID, poolID int64) error

	// UnlinkResource removes the pool link from a discovered resource.
	UnlinkResource(ctx context.Context, resourceID uuid.UUID) error

	// DeleteDiscoveredResource deletes a discovered resource by ID.
	DeleteDiscoveredResource(ctx context.Context, id uuid.UUID) error

	// CreateSyncJob creates a new sync job record.
	CreateSyncJob(ctx context.Context, job domain.SyncJob) (domain.SyncJob, error)

	// UpdateSyncJob updates an existing sync job (status, counts, error).
	UpdateSyncJob(ctx context.Context, job domain.SyncJob) error

	// GetSyncJob returns a sync job by ID.
	GetSyncJob(ctx context.Context, id uuid.UUID) (*domain.SyncJob, error)

	// ListSyncJobs returns recent sync jobs for an account, ordered by created_at desc.
	ListSyncJobs(ctx context.Context, accountID int64, limit int) ([]domain.SyncJob, error)

	// UpsertAgent inserts or updates a discovery agent (upserts by agent ID).
	UpsertAgent(ctx context.Context, agent domain.DiscoveryAgent) error

	// GetAgent returns a discovery agent by ID.
	GetAgent(ctx context.Context, id uuid.UUID) (*domain.DiscoveryAgent, error)

	// ListAgents returns all discovery agents, optionally filtered by account ID.
	ListAgents(ctx context.Context, accountID int64) ([]domain.DiscoveryAgent, error)

	// CreateBootstrapToken creates a new bootstrap token for agent registration.
	CreateBootstrapToken(ctx context.Context, token domain.BootstrapToken) (domain.BootstrapToken, error)

	// GetBootstrapToken returns a bootstrap token by ID.
	GetBootstrapToken(ctx context.Context, id string) (*domain.BootstrapToken, error)

	// GetBootstrapTokenByToken returns a bootstrap token by the actual token string (after hashing).
	GetBootstrapTokenByToken(ctx context.Context, tokenHash []byte) (*domain.BootstrapToken, error)

	// ListBootstrapTokens returns all bootstrap tokens.
	ListBootstrapTokens(ctx context.Context) ([]domain.BootstrapToken, error)

	// RevokeBootstrapToken marks a token as revoked.
	RevokeBootstrapToken(ctx context.Context, id string) error

	// IncrementBootstrapTokenUses increments the used count for a token.
	IncrementBootstrapTokenUses(ctx context.Context, id string) error
}
