package storage

import (
	"context"
	"errors"
	"net/netip"
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
	// UpdatePool updates pool metadata with support for new fields (type, status, description, tags).
	UpdatePool(ctx context.Context, id int64, update domain.UpdatePool) (domain.Pool, bool, error)
	DeletePool(ctx context.Context, id int64) (bool, error)
	DeletePoolCascade(ctx context.Context, id int64) (bool, error)
	// Enhanced pool methods for hierarchy and statistics
	GetPoolWithStats(ctx context.Context, id int64) (*domain.PoolWithStats, error)
	GetPoolHierarchy(ctx context.Context, rootID *int64) ([]domain.PoolWithStats, error)
	GetPoolChildren(ctx context.Context, parentID int64) ([]domain.Pool, error)
	CalculatePoolUtilization(ctx context.Context, id int64) (*domain.PoolStats, error)
	// Accounts management
	ListAccounts(ctx context.Context) ([]domain.Account, error)
	CreateAccount(ctx context.Context, in domain.CreateAccount) (domain.Account, error)
	UpdateAccount(ctx context.Context, id int64, update domain.Account) (domain.Account, bool, error)
	DeleteAccount(ctx context.Context, id int64) (bool, error)
	DeleteAccountCascade(ctx context.Context, id int64) (bool, error)
	GetAccount(ctx context.Context, id int64) (domain.Account, bool, error)
	// Close releases resources held by the store
	Close() error
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
	now := time.Now().UTC()

	// Apply defaults for new fields
	poolType := in.Type
	if poolType == "" {
		poolType = domain.PoolTypeSubnet
	}
	poolStatus := in.Status
	if poolStatus == "" {
		poolStatus = domain.PoolStatusActive
	}
	poolSource := in.Source
	if poolSource == "" {
		poolSource = domain.PoolSourceManual
	}

	// Copy tags to avoid shared reference
	var tags map[string]string
	if in.Tags != nil {
		tags = make(map[string]string, len(in.Tags))
		for k, v := range in.Tags {
			tags[k] = v
		}
	}

	p := domain.Pool{
		ID:          id,
		Name:        in.Name,
		CIDR:        in.CIDR,
		ParentID:    in.ParentID,
		AccountID:   in.AccountID,
		Type:        poolType,
		Status:      poolStatus,
		Source:      poolSource,
		Description: in.Description,
		Tags:        tags,
		CreatedAt:   now,
		UpdatedAt:   now,
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
	p.UpdatedAt = time.Now().UTC()
	m.pools[id] = p
	return p, true, nil
}

// UpdatePool updates pool metadata with support for new fields.
func (m *MemoryStore) UpdatePool(ctx context.Context, id int64, update domain.UpdatePool) (domain.Pool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok {
		return domain.Pool{}, false, nil
	}
	if update.Name != nil {
		p.Name = *update.Name
	}
	// AccountID is always set (can be nil to clear)
	p.AccountID = update.AccountID
	if update.Type != nil {
		p.Type = *update.Type
	}
	if update.Status != nil {
		p.Status = *update.Status
	}
	if update.Description != nil {
		p.Description = *update.Description
	}
	if update.Tags != nil {
		// Copy tags to avoid shared reference
		if *update.Tags != nil {
			p.Tags = make(map[string]string, len(*update.Tags))
			for k, v := range *update.Tags {
				p.Tags[k] = v
			}
		} else {
			p.Tags = nil
		}
	}
	p.UpdatedAt = time.Now().UTC()
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
	if update.Regions != nil {
		a.Regions = append([]string(nil), update.Regions...)
	}
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

func (m *MemoryStore) GetAccount(ctx context.Context, id int64) (domain.Account, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.accounts[id]
	return a, ok, nil
}

// GetPoolWithStats returns a pool with its computed statistics.
func (m *MemoryStore) GetPoolWithStats(ctx context.Context, id int64) (*domain.PoolWithStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	if !ok {
		return nil, errors.New("pool not found")
	}
	stats := m.calculatePoolStatsLocked(p)
	return &domain.PoolWithStats{
		Pool:  p,
		Stats: stats,
	}, nil
}

// GetPoolHierarchy returns the pool hierarchy tree starting from rootID.
// If rootID is nil, returns all top-level pools with their children.
func (m *MemoryStore) GetPoolHierarchy(ctx context.Context, rootID *int64) ([]domain.PoolWithStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Build parent -> children map
	children := make(map[int64][]int64)
	for _, p := range m.pools {
		if p.ParentID != nil {
			children[*p.ParentID] = append(children[*p.ParentID], p.ID)
		}
	}

	// Recursive function to build tree
	var buildTree func(pid int64) domain.PoolWithStats
	buildTree = func(pid int64) domain.PoolWithStats {
		p := m.pools[pid]
		stats := m.calculatePoolStatsLocked(p)
		result := domain.PoolWithStats{
			Pool:  p,
			Stats: stats,
		}
		for _, childID := range children[pid] {
			result.Children = append(result.Children, buildTree(childID))
		}
		return result
	}

	var result []domain.PoolWithStats

	if rootID != nil {
		// Return subtree from specific root
		if _, ok := m.pools[*rootID]; !ok {
			return nil, errors.New("root pool not found")
		}
		result = append(result, buildTree(*rootID))
	} else {
		// Return all top-level pools (no parent)
		for _, p := range m.pools {
			if p.ParentID == nil {
				result = append(result, buildTree(p.ID))
			}
		}
	}

	return result, nil
}

// GetPoolChildren returns the direct children of a pool.
func (m *MemoryStore) GetPoolChildren(ctx context.Context, parentID int64) ([]domain.Pool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.pools[parentID]; !ok {
		return nil, errors.New("parent pool not found")
	}

	var children []domain.Pool
	for _, p := range m.pools {
		if p.ParentID != nil && *p.ParentID == parentID {
			children = append(children, p)
		}
	}
	return children, nil
}

// CalculatePoolUtilization calculates statistics for a pool.
func (m *MemoryStore) CalculatePoolUtilization(ctx context.Context, id int64) (*domain.PoolStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	if !ok {
		return nil, errors.New("pool not found")
	}
	stats := m.calculatePoolStatsLocked(p)
	return &stats, nil
}

// calculatePoolStatsLocked calculates stats for a pool (must be called with lock held).
func (m *MemoryStore) calculatePoolStatsLocked(p domain.Pool) domain.PoolStats {
	// Parse the pool's CIDR to get total IPs
	prefix, err := netip.ParsePrefix(p.CIDR)
	if err != nil {
		return domain.PoolStats{}
	}

	var totalIPs int64
	if prefix.Addr().Is4() {
		totalIPs = int64(1) << (32 - prefix.Bits())
	} else {
		// For IPv6, cap at max int64 for practical purposes
		bits := 128 - prefix.Bits()
		if bits >= 63 {
			totalIPs = int64(1) << 62 // Cap to avoid overflow
		} else {
			totalIPs = int64(1) << bits
		}
	}

	// Count direct children and calculate used IPs
	var directChildren int
	var usedIPs int64
	var totalChildCount int

	// Recursive function to count all descendants
	var countDescendants func(parentID int64) int
	countDescendants = func(parentID int64) int {
		count := 0
		for _, child := range m.pools {
			if child.ParentID != nil && *child.ParentID == parentID {
				count++
				count += countDescendants(child.ID)
			}
		}
		return count
	}

	for _, child := range m.pools {
		if child.ParentID != nil && *child.ParentID == p.ID {
			directChildren++

			// Calculate used IPs from child's CIDR
			childPrefix, err := netip.ParsePrefix(child.CIDR)
			if err != nil {
				continue
			}
			if childPrefix.Addr().Is4() {
				usedIPs += int64(1) << (32 - childPrefix.Bits())
			}
		}
	}

	totalChildCount = countDescendants(p.ID)

	// Calculate utilization percentage
	var utilization float64
	if totalIPs > 0 {
		utilization = float64(usedIPs) / float64(totalIPs) * 100
	}

	return domain.PoolStats{
		TotalIPs:       totalIPs,
		UsedIPs:        usedIPs,
		AvailableIPs:   totalIPs - usedIPs,
		Utilization:    utilization,
		ChildCount:     totalChildCount,
		DirectChildren: directChildren,
	}
}

// Close is a no-op for MemoryStore as it holds no external resources.
func (m *MemoryStore) Close() error {
	return nil
}
