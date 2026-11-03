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

// lowUtilizationPercent is the utilization below which a pool with children counts as
// oversized, for fragmentation scoring and for resize recommendations alike.
const lowUtilizationPercent = 25.0

// shrinkTarget returns the smallest block inside pool that still holds every child whole,
// or false when the children already need the whole pool. A block that one child would fill
// completely is widened by one bit, since a pool must be larger than its children.
func shrinkTarget(pool netip.Prefix, children []netip.Prefix) (netip.Prefix, bool) {
	if len(children) == 0 {
		return netip.Prefix{}, false
	}
	lo, hi := uint32(0xffffffff), uint32(0)
	longest := 0
	for _, c := range children {
		iv := prefixToInterval(c)
		lo = min(lo, iv.start)
		hi = max(hi, iv.end)
		longest = max(longest, c.Bits())
	}
	var target netip.Prefix
	for bits := 32; bits >= pool.Bits(); bits-- {
		p := netip.PrefixFrom(uint32ToIPv4(lo), bits).Masked()
		if p.Contains(uint32ToIPv4(hi)) {
			target = p
			break
		}
	}
	if target.Bits() == longest {
		target = netip.PrefixFrom(target.Addr(), target.Bits()-1).Masked()
	}
	if target.Bits() <= pool.Bits() {
		return netip.Prefix{}, false
	}
	return target, true
}

// scoreResize rates a shrink 0-100: the lower the utilization and the more prefix bits the
// shrink frees, the higher the score.
func scoreResize(utilization float64, freedBits int) int {
	score := 40 + int((lowUtilizationPercent-utilization)/lowUtilizationPercent*30)
	score += min(freedBits, 3) * 10
	return min(score, 100)
}

// resizeRecommendation skips a pool linked to a discovered resource, since the resource still
// occupies the pool's whole recorded range whatever its child pools use.
func (s *RecommendationService) resizeRecommendation(ctx context.Context, pool domain.Pool, tree poolTree, linked map[int64][]uuid.UUID, now time.Time) *domain.Recommendation {
	children := tree.childrenOf(pool.ID)
	if len(children) == 0 || len(linked[pool.ID]) > 0 {
		return nil
	}
	stats, err := s.mainStore.CalculatePoolUtilization(ctx, pool.ID)
	if err != nil || stats == nil || stats.Utilization >= lowUtilizationPercent {
		return nil
	}
	target, ok := shrinkTargetFor(pool, children)
	if !ok {
		return nil
	}
	current, _ := netip.ParsePrefix(pool.CIDR)
	score := scoreResize(stats.Utilization, target.Bits()-current.Bits())
	return &domain.Recommendation{
		ID:            uuid.New().String(),
		PoolID:        pool.ID,
		Type:          domain.RecommendationTypeResize,
		Status:        domain.RecommendationStatusPending,
		Priority:      priorityFromScore(score),
		Title:         fmt.Sprintf("Shrink %s to %s", pool.CIDR, target),
		Description:   fmt.Sprintf("Pool %q uses %.1f%% of %s; its %d child pools fit in %s (%d addresses)", pool.Name, stats.Utilization, pool.CIDR, len(children), target, prefixAddressCount(target)),
		SuggestedCIDR: target.String(),
		Score:         score,
		Metadata: map[string]string{
			"current_cidr":        pool.CIDR,
			"utilization_percent": strconv.FormatFloat(stats.Utilization, 'f', 1, 64),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func shrinkTargetFor(pool domain.Pool, children []domain.Pool) (netip.Prefix, bool) {
	current, err := netip.ParsePrefix(pool.CIDR)
	if err != nil || !current.Addr().Is4() {
		return netip.Prefix{}, false
	}
	prefixes := make([]netip.Prefix, 0, len(children))
	for _, c := range children {
		p, err := netip.ParsePrefix(c.CIDR)
		if err != nil || !p.Addr().Is4() {
			return netip.Prefix{}, false
		}
		prefixes = append(prefixes, p.Masked())
	}
	return shrinkTarget(current.Masked(), prefixes)
}

// applyResize shrinks the pool to the suggested block under the placement guard, so no child
// can be placed in the range being given up, and no resource linked to the pool, between the
// check and the write. A block inside the
// pool's own range cannot leave the parent or overlap a sibling.
func (s *RecommendationService) applyResize(ctx context.Context, rec *domain.Recommendation) (*int64, error) {
	target, err := netip.ParsePrefix(rec.SuggestedCIDR)
	if err != nil {
		return nil, fmt.Errorf("resize recommendation %s has cidr %q: %w", rec.ID, rec.SuggestedCIDR, storage.ErrValidation)
	}
	err = s.placer.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		pools, err := ps.ListPools(work)
		if err != nil {
			return fmt.Errorf("list pools: %w", err)
		}
		if err := checkShrink(pools, rec.PoolID, rec.Metadata["current_cidr"], target); err != nil {
			return err
		}
		if err := s.rejectLinked(work, rec.PoolID); err != nil {
			return err
		}
		if _, found, err := ps.UpdatePoolCIDR(work, rec.PoolID, target.String()); err != nil {
			return fmt.Errorf("resize pool %d: %w", rec.PoolID, err)
		} else if !found {
			return fmt.Errorf("pool %d: %w", rec.PoolID, storage.ErrNotFound)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &rec.PoolID, nil
}

// checkShrink confirms the pool still has the range the recommendation saw, that target lies
// strictly inside it, that every child still fits in target, and that no other pool uses target.
func checkShrink(pools []domain.Pool, id int64, currentCIDR string, target netip.Prefix) error {
	var pool *domain.Pool
	for i := range pools {
		if pools[i].ID == id {
			pool = &pools[i]
		}
	}
	if pool == nil || pool.CIDR != currentCIDR {
		return fmt.Errorf("pool %d changed since the recommendation was generated: %w", id, storage.ErrConflict)
	}
	current, err := netip.ParsePrefix(pool.CIDR)
	if err != nil || target.Bits() <= current.Bits() || !current.Contains(target.Addr()) {
		return fmt.Errorf("resize target %s is not inside %s: %w", target, pool.CIDR, storage.ErrValidation)
	}
	for _, c := range newPoolTree(pools).childrenOf(id) {
		cp, err := netip.ParsePrefix(c.CIDR)
		if err != nil || cp.Bits() <= target.Bits() || !target.Contains(cp.Addr()) {
			return fmt.Errorf("child pool %d (%s) no longer fits in %s: %w", c.ID, c.CIDR, target, storage.ErrConflict)
		}
	}
	return rejectDuplicateCIDR(pools, target.String())
}
