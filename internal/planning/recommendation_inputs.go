package planning

import (
	"context"
	"fmt"

	"github.com/google/uuid"

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

// linkedResources returns the discovered resources linked to each pool, keyed by pool ID.
func (s *RecommendationService) linkedResources(ctx context.Context) (map[int64][]uuid.UUID, error) {
	accounts, err := s.mainStore.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	linked := true
	byPool := make(map[int64][]uuid.UUID)
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
					byPool[*r.PoolID] = append(byPool[*r.PoolID], r.ID)
				}
			}
			if len(items) == 0 || page*storage.MaxPageSize >= total {
				break
			}
		}
	}
	return byPool, nil
}

// relinkResources moves every discovered resource linked to pool from onto pool to.
func (s *RecommendationService) relinkResources(ctx context.Context, from, to int64) error {
	linked, err := s.linkedResources(ctx)
	if err != nil {
		return err
	}
	for _, id := range linked[from] {
		if err := s.discovery.LinkResourceToPool(ctx, id, to); err != nil {
			return fmt.Errorf("move resource %s from deleted pool %d to pool %d: %w", id, from, to, err)
		}
	}
	return nil
}
