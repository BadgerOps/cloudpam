package storage

import (
	"context"
	"sort"
	"time"

	"cloudpam/internal/domain"
)

// MemoryDriftStore is an in-memory implementation of DriftStore.
type MemoryDriftStore struct {
	store  *MemoryStore // shared mutex
	drifts map[string]domain.DriftItem
}

// NewMemoryDriftStore creates a new in-memory drift store.
func NewMemoryDriftStore(store *MemoryStore) *MemoryDriftStore {
	return &MemoryDriftStore{
		store:  store,
		drifts: make(map[string]domain.DriftItem),
	}
}

func (m *MemoryDriftStore) CreateDriftItem(_ context.Context, item domain.DriftItem) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	m.drifts[item.ID] = item
	return nil
}

func (m *MemoryDriftStore) GetDriftItem(_ context.Context, id string) (*domain.DriftItem, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	d, ok := m.drifts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &d, nil
}

func (m *MemoryDriftStore) ListDriftItems(_ context.Context, filters domain.DriftFilters) ([]domain.DriftItem, int, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	var filtered []domain.DriftItem
	for _, d := range m.drifts {
		if filters.AccountID != 0 && d.AccountID != filters.AccountID {
			continue
		}
		if filters.Type != "" && string(d.Type) != filters.Type {
			continue
		}
		if filters.Severity != "" && string(d.Severity) != filters.Severity {
			continue
		}
		if filters.Status != "" && string(d.Status) != filters.Status {
			continue
		}
		filtered = append(filtered, d)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].DetectedAt.After(filtered[j].DetectedAt)
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
		return []domain.DriftItem{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}

func (m *MemoryDriftStore) UpdateDriftStatus(_ context.Context, id string, status domain.DriftStatus, ignoreReason string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	d, ok := m.drifts[id]
	if !ok {
		return ErrNotFound
	}
	d.Status = status
	d.IgnoreReason = ignoreReason
	d.UpdatedAt = time.Now().UTC()
	if status == domain.DriftStatusResolved {
		now := time.Now().UTC()
		d.ResolvedAt = &now
	}
	m.drifts[id] = d
	return nil
}

func (m *MemoryDriftStore) DeleteOpenForAccount(_ context.Context, accountID int64) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	for id, d := range m.drifts {
		if d.AccountID == accountID && d.Status == domain.DriftStatusOpen {
			delete(m.drifts, id)
		}
	}
	return nil
}
