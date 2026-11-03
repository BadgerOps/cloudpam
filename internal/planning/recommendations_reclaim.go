package planning

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// reclaimIdleAfter is how long a pool must go unchanged before it counts as inactive.
const reclaimIdleAfter = 30 * 24 * time.Hour

// scoreReclaim rates a reclaim 0-100: the longer the pool sat idle and the larger the block
// it holds, the higher the score.
func scoreReclaim(idle time.Duration, bits int) int {
	score := 40
	switch {
	case idle >= 180*24*time.Hour:
		score += 30
	case idle >= 90*24*time.Hour:
		score += 20
	default:
		score += 10
	}
	switch {
	case bits <= 16:
		score += 30
	case bits <= 20:
		score += 20
	case bits <= 24:
		score += 10
	}
	return min(score, 100)
}

func (s *RecommendationService) reclaimRecommendation(pool domain.Pool, tree poolTree, linked map[int64][]uuid.UUID, now time.Time) *domain.Recommendation {
	if !tree.isLeaf(pool.ID) || len(linked[pool.ID]) > 0 {
		return nil
	}
	idle := now.Sub(pool.UpdatedAt)
	if idle < reclaimIdleAfter {
		return nil
	}
	prefix, err := netip.ParsePrefix(pool.CIDR)
	if err != nil {
		return nil
	}
	idleDays := int(idle.Hours() / 24)
	score := scoreReclaim(idle, prefix.Bits())
	return &domain.Recommendation{
		ID:            uuid.New().String(),
		PoolID:        pool.ID,
		Type:          domain.RecommendationTypeReclaim,
		Status:        domain.RecommendationStatusPending,
		Priority:      priorityFromScore(score),
		Title:         fmt.Sprintf("Reclaim %s", pool.CIDR),
		Description:   fmt.Sprintf("Pool %q has no child pools, no linked discovered resources and no changes for %d days; deleting it frees %d addresses", pool.Name, idleDays, prefixAddressCount(prefix)),
		SuggestedCIDR: pool.CIDR,
		Score:         score,
		Metadata: map[string]string{
			"idle_days":    strconv.Itoa(idleDays),
			"last_updated": pool.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// applyReclaim deletes the pool once it is still the idle, unlinked leaf the recommendation saw.
// The checks and the delete run under the placement guard, which pool placement and resource
// linking also hold, so no child or link can land between them.
func (s *RecommendationService) applyReclaim(ctx context.Context, rec *domain.Recommendation) error {
	return s.placer.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		pools, err := ps.ListPools(work)
		if err != nil {
			return fmt.Errorf("list pools: %w", err)
		}
		if err := checkReclaimable(pools, rec); err != nil {
			return err
		}
		if err := s.rejectLinked(work, ps, rec.PoolID); err != nil {
			return err
		}
		deleted, err := ps.DeletePool(work, rec.PoolID)
		if err != nil {
			return fmt.Errorf("delete pool %d: %w", rec.PoolID, err)
		}
		if !deleted {
			return fmt.Errorf("pool %d: %w", rec.PoolID, storage.ErrNotFound)
		}
		return nil
	})
}

// checkReclaimable confirms the pool is live, still has the CIDR the recommendation recorded,
// has not been updated since, and has no children.
func checkReclaimable(pools []domain.Pool, rec *domain.Recommendation) error {
	var pool *domain.Pool
	for i := range pools {
		if pools[i].ID == rec.PoolID {
			pool = &pools[i]
		}
	}
	if pool == nil {
		return fmt.Errorf("pool %d: %w", rec.PoolID, storage.ErrNotFound)
	}
	if pool.CIDR != rec.SuggestedCIDR || pool.UpdatedAt.UTC().Format(time.RFC3339Nano) != rec.Metadata["last_updated"] {
		return fmt.Errorf("pool %d changed since the recommendation was generated: %w", rec.PoolID, storage.ErrConflict)
	}
	if !newPoolTree(pools).isLeaf(rec.PoolID) {
		return fmt.Errorf("pool %d now has child pools: %w", rec.PoolID, storage.ErrConflict)
	}
	return nil
}
