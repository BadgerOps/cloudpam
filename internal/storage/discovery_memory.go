package storage

import (
	"context"
	"crypto/subtle"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// MemoryDiscoveryStore is an in-memory implementation of DiscoveryStore.
type MemoryDiscoveryStore struct {
	store           *MemoryStore // embed reference for shared mutex
	resources       map[uuid.UUID]domain.DiscoveredResource
	syncJobs        map[uuid.UUID]domain.SyncJob
	agents          map[uuid.UUID]domain.DiscoveryAgent
	bootstrapTokens map[string]domain.BootstrapToken
}

// NewMemoryDiscoveryStore creates a new in-memory discovery store.
func NewMemoryDiscoveryStore(store *MemoryStore) *MemoryDiscoveryStore {
	return &MemoryDiscoveryStore{
		store:           store,
		resources:       make(map[uuid.UUID]domain.DiscoveredResource),
		syncJobs:        make(map[uuid.UUID]domain.SyncJob),
		agents:          make(map[uuid.UUID]domain.DiscoveryAgent),
		bootstrapTokens: make(map[string]domain.BootstrapToken),
	}
}

func (m *MemoryDiscoveryStore) ListDiscoveredResources(_ context.Context, accountID int64, filters domain.DiscoveryFilters) ([]domain.DiscoveredResource, int, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	var filtered []domain.DiscoveredResource
	for _, r := range m.resources {
		if r.AccountID != accountID {
			continue
		}
		if filters.Provider != "" && r.Provider != filters.Provider {
			continue
		}
		if filters.Region != "" && r.Region != filters.Region {
			continue
		}
		if filters.ResourceType != "" && string(r.ResourceType) != filters.ResourceType {
			continue
		}
		if filters.Status != "" && string(r.Status) != filters.Status {
			continue
		}
		if filters.HasPool != nil {
			linked := r.PoolID != nil
			if *filters.HasPool != linked {
				continue
			}
		}
		filtered = append(filtered, r)
	}

	// Sort by discovered_at desc
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].DiscoveredAt.After(filtered[j].DiscoveredAt)
	})

	total := len(filtered)

	// Paginate
	page := filters.Page
	if page < 1 {
		page = 1
	}
	pageSize := filters.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	start := (page - 1) * pageSize
	if start > total {
		return []domain.DiscoveredResource{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}

func (m *MemoryDiscoveryStore) GetDiscoveredResource(_ context.Context, id uuid.UUID) (*domain.DiscoveredResource, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	r, ok := m.resources[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (m *MemoryDiscoveryStore) UpsertDiscoveredResource(_ context.Context, res domain.DiscoveredResource) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	// Check for existing by (account_id, resource_id)
	for id, existing := range m.resources {
		if existing.AccountID == res.AccountID && existing.ResourceID == res.ResourceID {
			// Update existing: preserve ID, pool link, and discovered_at
			res.ID = id
			if res.PoolID == nil {
				res.PoolID = existing.PoolID
			}
			if res.DiscoveredAt.IsZero() {
				res.DiscoveredAt = existing.DiscoveredAt
			}
			m.resources[id] = res
			return nil
		}
	}

	// Insert new
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	m.resources[res.ID] = res
	return nil
}

func (m *MemoryDiscoveryStore) MarkStaleResources(_ context.Context, accountID int64, before time.Time) (int, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	count := 0
	for id, r := range m.resources {
		if r.AccountID == accountID && r.Status == domain.DiscoveryStatusActive && r.LastSeenAt.Before(before) {
			r.Status = domain.DiscoveryStatusStale
			m.resources[id] = r
			count++
		}
	}
	return count, nil
}

func (m *MemoryDiscoveryStore) LinkResourceToPool(_ context.Context, resourceID uuid.UUID, poolID int64) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	r, ok := m.resources[resourceID]
	if !ok {
		return fmt.Errorf("resource not found: %w", ErrNotFound)
	}
	r.PoolID = &poolID
	m.resources[resourceID] = r
	return nil
}

func (m *MemoryDiscoveryStore) UnlinkResource(_ context.Context, resourceID uuid.UUID) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	r, ok := m.resources[resourceID]
	if !ok {
		return fmt.Errorf("resource not found: %w", ErrNotFound)
	}
	r.PoolID = nil
	m.resources[resourceID] = r
	return nil
}

func (m *MemoryDiscoveryStore) DeleteDiscoveredResource(_ context.Context, id uuid.UUID) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	if _, ok := m.resources[id]; !ok {
		return ErrNotFound
	}
	delete(m.resources, id)
	return nil
}

func (m *MemoryDiscoveryStore) CreateSyncJob(_ context.Context, job domain.SyncJob) (domain.SyncJob, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	m.syncJobs[job.ID] = job
	return job, nil
}

func (m *MemoryDiscoveryStore) UpdateSyncJob(_ context.Context, job domain.SyncJob) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	if _, ok := m.syncJobs[job.ID]; !ok {
		return ErrNotFound
	}
	m.syncJobs[job.ID] = job
	return nil
}

func (m *MemoryDiscoveryStore) GetSyncJob(_ context.Context, id uuid.UUID) (*domain.SyncJob, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	j, ok := m.syncJobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &j, nil
}

func (m *MemoryDiscoveryStore) ListSyncJobs(_ context.Context, accountID int64, limit int) ([]domain.SyncJob, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	var jobs []domain.SyncJob
	for _, j := range m.syncJobs {
		if j.AccountID == accountID {
			jobs = append(jobs, j)
		}
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (m *MemoryDiscoveryStore) UpsertAgent(_ context.Context, agent domain.DiscoveryAgent) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	if agent.ID == uuid.Nil {
		agent.ID = uuid.New()
	}
	if agent.CreatedAt.IsZero() {
		// Preserve existing creation time if already exists
		if existing, ok := m.agents[agent.ID]; ok {
			agent.CreatedAt = existing.CreatedAt
		} else {
			agent.CreatedAt = time.Now().UTC()
		}
	}
	m.agents[agent.ID] = agent
	return nil
}

func (m *MemoryDiscoveryStore) GetAgent(_ context.Context, id uuid.UUID) (*domain.DiscoveryAgent, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	a, ok := m.agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &a, nil
}

func (m *MemoryDiscoveryStore) ListAgents(_ context.Context, accountID int64) ([]domain.DiscoveryAgent, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	var agents []domain.DiscoveryAgent
	for _, a := range m.agents {
		if accountID == 0 || a.AccountID == accountID {
			agents = append(agents, a)
		}
	}

	// Sort by created_at desc
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].CreatedAt.After(agents[j].CreatedAt)
	})

	return agents, nil
}

func (m *MemoryDiscoveryStore) CreateBootstrapToken(_ context.Context, token domain.BootstrapToken) (domain.BootstrapToken, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}

	m.bootstrapTokens[token.ID] = token
	return token, nil
}

func (m *MemoryDiscoveryStore) GetBootstrapToken(_ context.Context, id string) (*domain.BootstrapToken, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	token, ok := m.bootstrapTokens[id]
	if !ok {
		return nil, ErrNotFound
	}
	// Don't return the plaintext token
	token.Token = ""
	return &token, nil
}

func (m *MemoryDiscoveryStore) GetBootstrapTokenByToken(_ context.Context, tokenHash []byte) (*domain.BootstrapToken, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	// Linear search through tokens to find matching hash
	for _, t := range m.bootstrapTokens {
		if subtle.ConstantTimeCompare(t.TokenHash, tokenHash) == 1 {
			t.Token = "" // Don't return plaintext
			return &t, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryDiscoveryStore) ListBootstrapTokens(_ context.Context) ([]domain.BootstrapToken, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	var tokens []domain.BootstrapToken
	for _, t := range m.bootstrapTokens {
		t.Token = "" // Don't return plaintext
		tokens = append(tokens, t)
	}

	// Sort by created_at desc
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i].CreatedAt.After(tokens[j].CreatedAt)
	})

	return tokens, nil
}

func (m *MemoryDiscoveryStore) RevokeBootstrapToken(_ context.Context, id string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	token, ok := m.bootstrapTokens[id]
	if !ok {
		return ErrNotFound
	}

	token.Revoked = true
	m.bootstrapTokens[id] = token
	return nil
}

func (m *MemoryDiscoveryStore) IncrementBootstrapTokenUses(_ context.Context, id string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	token, ok := m.bootstrapTokens[id]
	if !ok {
		return ErrNotFound
	}

	token.UsedCount++
	m.bootstrapTokens[id] = token
	return nil
}
