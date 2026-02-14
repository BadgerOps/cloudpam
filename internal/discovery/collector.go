// Package discovery provides cloud resource discovery and sync capabilities.
package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// Collector discovers cloud resources for a given account.
type Collector interface {
	// Provider returns the cloud provider name (e.g., "aws", "gcp", "azure").
	Provider() string
	// Discover returns all discovered resources for the given account.
	Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error)
}

// SyncService orchestrates discovery sync runs.
type SyncService struct {
	store      storage.DiscoveryStore
	collectors map[string]Collector
}

// NewSyncService creates a new sync service with the given discovery store.
func NewSyncService(store storage.DiscoveryStore) *SyncService {
	return &SyncService{
		store:      store,
		collectors: make(map[string]Collector),
	}
}

// RegisterCollector registers a collector for a cloud provider.
func (s *SyncService) RegisterCollector(c Collector) {
	s.collectors[c.Provider()] = c
}

// Sync runs a discovery sync for the given account.
// It creates a SyncJob, runs the appropriate collector, upserts resources,
// and marks stale resources.
func (s *SyncService) Sync(ctx context.Context, account domain.Account) (*domain.SyncJob, error) {
	provider := account.Provider
	collector, ok := s.collectors[provider]
	if !ok {
		return nil, fmt.Errorf("no collector registered for provider %q", provider)
	}

	now := time.Now().UTC()
	job := domain.SyncJob{
		ID:        uuid.New(),
		AccountID: account.ID,
		Status:    domain.SyncJobStatusRunning,
		StartedAt: &now,
		CreatedAt: now,
	}
	job, err := s.store.CreateSyncJob(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("create sync job: %w", err)
	}

	// Run discovery
	resources, discoverErr := collector.Discover(ctx, account)

	if discoverErr != nil {
		completedAt := time.Now().UTC()
		job.Status = domain.SyncJobStatusFailed
		job.CompletedAt = &completedAt
		job.ErrorMessage = discoverErr.Error()
		_ = s.store.UpdateSyncJob(ctx, job)
		return &job, fmt.Errorf("discovery failed: %w", discoverErr)
	}

	// Upsert discovered resources
	created, updated := 0, 0
	for _, res := range resources {
		// Check if resource already exists
		existing, _ := s.findByAccountAndResourceID(ctx, account.ID, res.ResourceID)
		if existing != nil {
			updated++
		} else {
			created++
		}
		if err := s.store.UpsertDiscoveredResource(ctx, res); err != nil {
			// Log and continue — don't fail the entire sync
			job.ErrorMessage = fmt.Sprintf("upsert error: %v", err)
		}
	}

	// Mark resources not seen in this run as stale
	staleCount, _ := s.store.MarkStaleResources(ctx, account.ID, now)

	completedAt := time.Now().UTC()
	job.Status = domain.SyncJobStatusCompleted
	job.CompletedAt = &completedAt
	job.ResourcesFound = len(resources)
	job.ResourcesCreated = created
	job.ResourcesUpdated = updated
	job.ResourcesDeleted = staleCount
	_ = s.store.UpdateSyncJob(ctx, job)

	return &job, nil
}

// findByAccountAndResourceID checks if a resource exists (helper for counting creates vs updates).
func (s *SyncService) findByAccountAndResourceID(ctx context.Context, accountID int64, resourceID string) (*domain.DiscoveredResource, error) {
	resources, _, err := s.store.ListDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range resources {
		if r.ResourceID == resourceID {
			return &r, nil
		}
	}
	return nil, nil
}
