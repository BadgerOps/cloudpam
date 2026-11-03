package planning

import (
	"context"
	"fmt"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// poolTree indexes the live pools by parent so the detectors answer "children of" and
// "is a leaf" from one ListPools snapshot instead of one store call per pool.
type poolTree struct {
	children map[int64][]domain.Pool
}

func newPoolTree(pools []domain.Pool) poolTree {
	t := poolTree{children: make(map[int64][]domain.Pool)}
	for _, p := range pools {
		if p.ParentID != nil {
			t.children[*p.ParentID] = append(t.children[*p.ParentID], p)
		}
	}
	return t
}

func (t poolTree) childrenOf(id int64) []domain.Pool { return t.children[id] }

func (t poolTree) isLeaf(id int64) bool { return len(t.children[id]) == 0 }

func (s *RecommendationService) loadPoolTree(ctx context.Context) (poolTree, error) {
	pools, err := s.mainStore.ListPools(ctx)
	if err != nil {
		return poolTree{}, fmt.Errorf("list pools: %w", err)
	}
	return newPoolTree(pools), nil
}

// linkedPoolIDs returns the IDs of every pool that a discovered resource is linked to.
func (s *RecommendationService) linkedPoolIDs(ctx context.Context) (map[int64]bool, error) {
	accounts, err := s.mainStore.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	linked := true
	ids := make(map[int64]bool)
	for _, acct := range accounts {
		for page := 1; ; page++ {
			items, total, err := s.discovery.ListDiscoveredResources(ctx, acct.ID, domain.DiscoveryFilters{
				HasPool:  &linked,
				Page:     page,
				PageSize: storage.MaxPageSize,
			})
			if err != nil {
				return nil, fmt.Errorf("list linked resources for account %d: %w", acct.ID, err)
			}
			for _, r := range items {
				if r.PoolID != nil {
					ids[*r.PoolID] = true
				}
			}
			if len(items) == 0 || page*storage.MaxPageSize >= total {
				break
			}
		}
	}
	return ids, nil
}
