package storage

import (
	"context"
	"sort"
	"time"

	"cloudpam/internal/domain"
)

// MemoryRecommendationStore is an in-memory implementation of RecommendationStore.
type MemoryRecommendationStore struct {
	store *MemoryStore // shared mutex
	recs  map[string]domain.Recommendation
}

// NewMemoryRecommendationStore creates a new in-memory recommendation store.
func NewMemoryRecommendationStore(store *MemoryStore) *MemoryRecommendationStore {
	return &MemoryRecommendationStore{
		store: store,
		recs:  make(map[string]domain.Recommendation),
	}
}

func (m *MemoryRecommendationStore) CreateRecommendation(_ context.Context, rec domain.Recommendation) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	m.recs[rec.ID] = rec
	return nil
}

func (m *MemoryRecommendationStore) GetRecommendation(_ context.Context, id string) (*domain.Recommendation, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	r, ok := m.recs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (m *MemoryRecommendationStore) ListRecommendations(_ context.Context, filters domain.RecommendationFilters) ([]domain.Recommendation, int, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	var filtered []domain.Recommendation
	for _, r := range m.recs {
		if filters.PoolID != 0 && r.PoolID != filters.PoolID {
			continue
		}
		if filters.Type != "" && string(r.Type) != filters.Type {
			continue
		}
		if filters.Status != "" && string(r.Status) != filters.Status {
			continue
		}
		if filters.Priority != "" && string(r.Priority) != filters.Priority {
			continue
		}
		filtered = append(filtered, r)
	}

	// Sort by created_at desc
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := len(filtered)

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
		return []domain.Recommendation{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}

func (m *MemoryRecommendationStore) UpdateRecommendationStatus(_ context.Context, id string, status domain.RecommendationStatus, dismissReason string, appliedPoolID *int64) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	r, ok := m.recs[id]
	if !ok {
		return ErrNotFound
	}
	r.Status = status
	r.DismissReason = dismissReason
	r.AppliedPoolID = appliedPoolID
	r.UpdatedAt = time.Now().UTC()
	m.recs[id] = r
	return nil
}

func (m *MemoryRecommendationStore) DeletePendingForPool(_ context.Context, poolID int64) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	for id, r := range m.recs {
		if r.PoolID == poolID && r.Status == domain.RecommendationStatusPending {
			delete(m.recs, id)
		}
	}
	return nil
}
