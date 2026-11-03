package storage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cloudpam/internal/domain"
)

func (m *MemoryStore) ListDeletedPools(ctx context.Context) ([]domain.Pool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.Pool{}
	for _, p := range m.pools {
		if p.DeletedAt != nil {
			out = append(out, clonePool(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return deletedFirst(out[i].DeletedAt, out[j].DeletedAt, out[i].ID, out[j].ID) })
	return out, nil
}

func (m *MemoryStore) RestorePool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok || p.DeletedAt == nil {
		return domain.Pool{}, false, nil
	}
	if p.ParentID != nil {
		if parent, ok := m.pools[*p.ParentID]; !ok || parent.DeletedAt != nil {
			return domain.Pool{}, false, fmt.Errorf("parent pool %d is deleted; restore it first: %w", *p.ParentID, ErrConflict)
		}
	}
	if p.AccountID != nil {
		if a, ok := m.accounts[*p.AccountID]; !ok || a.DeletedAt != nil {
			return domain.Pool{}, false, fmt.Errorf("account %d is deleted; restore it first: %w", *p.AccountID, ErrConflict)
		}
	}
	for _, other := range m.pools {
		if other.DeletedAt == nil && other.CIDR == p.CIDR && sameParentID(other.ParentID, p.ParentID) {
			return domain.Pool{}, false, fmt.Errorf("pool %q already exists under this parent: %w", p.CIDR, ErrConflict)
		}
	}
	p.DeletedAt = nil
	p.UpdatedAt = time.Now().UTC()
	m.pools[id] = p
	return clonePool(p), true, nil
}

func (m *MemoryStore) ListDeletedAccounts(ctx context.Context) ([]domain.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.Account{}
	for _, a := range m.accounts {
		if a.DeletedAt != nil {
			out = append(out, cloneAccount(a))
		}
	}
	sort.Slice(out, func(i, j int) bool { return deletedFirst(out[i].DeletedAt, out[j].DeletedAt, out[i].ID, out[j].ID) })
	return out, nil
}

func (m *MemoryStore) RestoreAccount(ctx context.Context, id int64) (domain.Account, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok || a.DeletedAt == nil {
		return domain.Account{}, false, nil
	}
	for _, other := range m.accounts {
		if other.DeletedAt == nil && other.Key == a.Key {
			return domain.Account{}, false, fmt.Errorf("account key %q already exists: %w", a.Key, ErrConflict)
		}
	}
	a.DeletedAt = nil
	a.UpdatedAt = time.Now().UTC()
	m.accounts[id] = a
	return cloneAccount(a), true, nil
}

func deletedFirst(a, b *time.Time, idA, idB int64) bool {
	if !a.Equal(*b) {
		return a.After(*b)
	}
	return idA > idB
}

func sameParentID(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
