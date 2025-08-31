package storage

import (
	"context"
	"errors"
	"sync"
	"time"

	"cloudpam/internal/domain"
)

// Store is the minimal storage interface for early development.
type Store interface {
	ListPools(ctx context.Context) ([]domain.Pool, error)
	CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error)
	GetPool(ctx context.Context, id int64) (domain.Pool, bool, error)
	UpdatePoolAccount(ctx context.Context, id int64, accountID *int64) (domain.Pool, bool, error)
	UpdatePoolMeta(ctx context.Context, id int64, name *string, accountID *int64) (domain.Pool, bool, error)
	DeletePool(ctx context.Context, id int64) (bool, error)
	DeletePoolCascade(ctx context.Context, id int64) (bool, error)
	// Accounts management
	ListAccounts(ctx context.Context) ([]domain.Account, error)
	CreateAccount(ctx context.Context, in domain.CreateAccount) (domain.Account, error)
	UpdateAccount(ctx context.Context, id int64, update domain.Account) (domain.Account, bool, error)
	DeleteAccount(ctx context.Context, id int64) (bool, error)
	DeleteAccountCascade(ctx context.Context, id int64) (bool, error)
}

// MemoryStore is an in-memory implementation for quick start and tests.
type MemoryStore struct {
	mu    sync.RWMutex
	pools map[int64]domain.Pool
	next  int64
	// accounts
	accounts    map[int64]domain.Account
	nextAccount int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{pools: make(map[int64]domain.Pool), next: 1, accounts: make(map[int64]domain.Account), nextAccount: 1}
}

func (m *MemoryStore) ListPools(ctx context.Context) ([]domain.Pool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Pool, 0, len(m.pools))
	for _, p := range m.pools {
		out = append(out, p)
	}
	return out, nil
}

func (m *MemoryStore) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	if in.Name == "" || in.CIDR == "" {
		return domain.Pool{}, errors.New("name and cidr required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.next
	m.next++
	p := domain.Pool{
		ID:        id,
		Name:      in.Name,
		CIDR:      in.CIDR,
		ParentID:  in.ParentID,
		AccountID: in.AccountID,
		CreatedAt: time.Now().UTC(),
	}
	m.pools[id] = p
	return p, nil
}

func (m *MemoryStore) GetPool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	return p, ok, nil
}

func (m *MemoryStore) UpdatePoolAccount(ctx context.Context, id int64, accountID *int64) (domain.Pool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok {
		return domain.Pool{}, false, nil
	}
	p.AccountID = accountID
	m.pools[id] = p
	return p, true, nil
}

func (m *MemoryStore) UpdatePoolMeta(ctx context.Context, id int64, name *string, accountID *int64) (domain.Pool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok {
		return domain.Pool{}, false, nil
	}
	if name != nil {
		p.Name = *name
	}
	// Always set account if provided (including clearing with nil)
	if accountID != nil || true {
		p.AccountID = accountID
	}
	m.pools[id] = p
	return p, true, nil
}

func (m *MemoryStore) DeletePool(ctx context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		if p.ParentID != nil && *p.ParentID == id {
			return false, errors.New("pool has child pools")
		}
	}
	if _, ok := m.pools[id]; !ok {
		return false, nil
	}
	delete(m.pools, id)
	return true, nil
}

func (m *MemoryStore) DeletePoolCascade(ctx context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Collect subtree ids starting from id
	if _, ok := m.pools[id]; !ok {
		return false, nil
	}
	toDelete := map[int64]struct{}{id: {}}
	progressed := true
	for progressed {
		progressed = false
		for pid, p := range m.pools {
			if _, seen := toDelete[pid]; seen {
				continue
			}
			if p.ParentID != nil {
				if _, ok := toDelete[*p.ParentID]; ok {
					toDelete[pid] = struct{}{}
					progressed = true
				}
			}
		}
	}
	for pid := range toDelete {
		delete(m.pools, pid)
	}
	return true, nil
}

// Accounts
func (m *MemoryStore) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Account, 0, len(m.accounts))
	for _, a := range m.accounts {
		out = append(out, a)
	}
	return out, nil
}

func (m *MemoryStore) CreateAccount(ctx context.Context, in domain.CreateAccount) (domain.Account, error) {
	if in.Key == "" || in.Name == "" {
		return domain.Account{}, errors.New("key and name required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextAccount
	m.nextAccount++
    a := domain.Account{
        ID:          id,
        Key:         in.Key,
        Name:        in.Name,
        Provider:    in.Provider,
        ExternalID:  in.ExternalID,
        Description: in.Description,
        Platform:    in.Platform,
        Tier:        in.Tier,
        Environment: in.Environment,
        Regions:     append([]string(nil), in.Regions...),
        CreatedAt:   time.Now().UTC(),
    }
	m.accounts[id] = a
	return a, nil
}

func (m *MemoryStore) UpdateAccount(ctx context.Context, id int64, update domain.Account) (domain.Account, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return domain.Account{}, false, nil
	}
	if update.Name != "" {
		a.Name = update.Name
	}
    // Allow empty strings to clear optional fields
    a.Provider = update.Provider
    a.ExternalID = update.ExternalID
    a.Description = update.Description
    a.Platform = update.Platform
    a.Tier = update.Tier
    a.Environment = update.Environment
    if update.Regions != nil { a.Regions = append([]string(nil), update.Regions...) }
	m.accounts[id] = a
	return a, true, nil
}

func (m *MemoryStore) DeleteAccount(ctx context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		if p.AccountID != nil && *p.AccountID == id {
			return false, errors.New("account in use by pools")
		}
	}
	if _, ok := m.accounts[id]; !ok {
		return false, nil
	}
	delete(m.accounts, id)
	return true, nil
}

func (m *MemoryStore) DeleteAccountCascade(ctx context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		return false, nil
	}
	// Gather pools referencing this account and their descendants
	toDelete := map[int64]struct{}{}
	for pid, p := range m.pools {
		if p.AccountID != nil && *p.AccountID == id {
			toDelete[pid] = struct{}{}
		}
	}
	progressed := true
	for progressed {
		progressed = false
		for pid, p := range m.pools {
			if _, seen := toDelete[pid]; seen {
				continue
			}
			if p.ParentID != nil {
				if _, ok := toDelete[*p.ParentID]; ok {
					toDelete[pid] = struct{}{}
					progressed = true
				}
			}
		}
	}
	for pid := range toDelete {
		delete(m.pools, pid)
	}
	delete(m.accounts, id)
	return true, nil
}
