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

func (s *RecommendationService) reclaimRecommendation(pool domain.Pool, tree poolTree, linked map[int64]bool, now time.Time) *domain.Recommendation {
	if !tree.isLeaf(pool.ID) || linked[pool.ID] {
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
			"last_updated": pool.UpdatedAt.UTC().Format(time.RFC3339),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// applyReclaim deletes the pool once it is still empty and unlinked.
func (s *RecommendationService) applyReclaim(ctx context.Context, rec *domain.Recommendation) error {
	children, err := s.mainStore.GetPoolChildren(ctx, rec.PoolID)
	if err != nil {
		return fmt.Errorf("get children of pool %d: %w", rec.PoolID, err)
	}
	if len(children) > 0 {
		return fmt.Errorf("pool %d now has child pools: %w", rec.PoolID, storage.ErrConflict)
	}
	linked, err := s.linkedPoolIDs(ctx)
	if err != nil {
		return err
	}
	if linked[rec.PoolID] {
		return fmt.Errorf("pool %d now has linked discovered resources: %w", rec.PoolID, storage.ErrConflict)
	}
	deleted, err := s.mainStore.DeletePool(ctx, rec.PoolID)
	if err != nil {
		return fmt.Errorf("delete pool %d: %w", rec.PoolID, err)
	}
	if !deleted {
		return fmt.Errorf("pool %d: %w", rec.PoolID, storage.ErrNotFound)
	}
	return nil
}
