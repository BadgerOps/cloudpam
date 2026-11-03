package planning

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// mergeCandidate is a pair of sibling leaf pools that are the two halves of one aligned block.
type mergeCandidate struct {
	first  domain.Pool
	second domain.Pool
	merged netip.Prefix
}

// mergeCandidates finds every pair of children that can collapse into one pool: the same
// prefix length, the two halves of a block aligned on that length, both without children,
// both assigned to the same account, and a merged block that is still smaller than the parent.
func mergeCandidates(parent netip.Prefix, children []domain.Pool, isLeaf func(int64) bool) []mergeCandidate {
	type key struct {
		start uint32
		bits  int
	}
	byStart := make(map[key]domain.Pool, len(children))
	prefixes := make(map[int64]netip.Prefix, len(children))
	for _, c := range children {
		p, err := netip.ParsePrefix(c.CIDR)
		if err != nil || !p.Addr().Is4() {
			continue
		}
		p = p.Masked()
		prefixes[c.ID] = p
		byStart[key{ipv4ToUint32(p.Addr()), p.Bits()}] = c
	}

	var out []mergeCandidate
	for _, first := range children {
		p, ok := prefixes[first.ID]
		if !ok || p.Bits() <= parent.Bits()+1 {
			continue
		}
		size := uint32(1) << (32 - p.Bits())
		start := ipv4ToUint32(p.Addr())
		if start%(2*size) != 0 {
			continue
		}
		second, ok := byStart[key{start + size, p.Bits()}]
		if !ok || !isLeaf(first.ID) || !isLeaf(second.ID) || !sameAccount(first, second) {
			continue
		}
		out = append(out, mergeCandidate{
			first:  first,
			second: second,
			merged: netip.PrefixFrom(p.Addr(), p.Bits()-1).Masked(),
		})
	}
	return out
}

func sameAccount(a, b domain.Pool) bool {
	if a.AccountID == nil || b.AccountID == nil {
		return a.AccountID == nil && b.AccountID == nil
	}
	return *a.AccountID == *b.AccountID
}

// scoreConsolidation rates a merge 0-100: a fragmented parent and small source blocks
// make the merge worth more.
func scoreConsolidation(fragmentationScore, sourceBits int) int {
	score := 40 + fragmentationScore*30/100
	switch {
	case sourceBits >= 26:
		score += 30
	case sourceBits >= 24:
		score += 20
	case sourceBits >= 20:
		score += 10
	}
	return min(score, 100)
}

func (s *RecommendationService) consolidationRecommendations(ctx context.Context, pool domain.Pool, tree poolTree, now time.Time) []domain.Recommendation {
	parent, err := netip.ParsePrefix(pool.CIDR)
	if err != nil {
		return nil
	}
	candidates := mergeCandidates(parent.Masked(), tree.childrenOf(pool.ID), tree.isLeaf)
	if len(candidates) == 0 {
		return nil
	}
	fragmentation := 0
	if frag, err := s.analysis.AnalyzeFragmentation(ctx, pool.ID); err == nil && frag != nil {
		fragmentation = frag.Score
	}

	recs := make([]domain.Recommendation, 0, len(candidates))
	for _, c := range candidates {
		score := scoreConsolidation(fragmentation, c.merged.Bits()+1)
		recs = append(recs, domain.Recommendation{
			ID:            uuid.New().String(),
			PoolID:        pool.ID,
			Type:          domain.RecommendationTypeConsolidation,
			Status:        domain.RecommendationStatusPending,
			Priority:      priorityFromScore(score),
			Title:         fmt.Sprintf("Merge %s and %s into %s", c.first.CIDR, c.second.CIDR, c.merged),
			Description:   fmt.Sprintf("Pools %q and %q are the two halves of %s and have no child pools; merging them removes one allocation from pool %q (fragmentation score %d)", c.first.Name, c.second.Name, c.merged, pool.Name, fragmentation),
			SuggestedCIDR: c.merged.String(),
			Score:         score,
			Metadata: map[string]string{
				"source_pool_ids":     fmt.Sprintf("%d,%d", c.first.ID, c.second.ID),
				"source_cidrs":        c.first.CIDR + "," + c.second.CIDR,
				"fragmentation_score": strconv.Itoa(fragmentation),
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return recs
}

// applyConsolidation widens the first source pool to the merged block and deletes the second,
// under the placement guard so no placement runs between the checks and the writes. Widening
// before deleting means the merged range is never free for another pool to take. The kept pool
// keeps its ID, name and tags; discovered resources linked to the deleted pool move to it.
func (s *RecommendationService) applyConsolidation(ctx context.Context, rec *domain.Recommendation, req domain.ApplyRecommendationRequest) (*int64, error) {
	ids, cidrs, err := consolidationSources(rec)
	if err != nil {
		return nil, err
	}
	var kept domain.Pool
	err = s.placer.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		pools, err := ps.ListPools(work)
		if err != nil {
			return fmt.Errorf("list pools: %w", err)
		}
		if err := checkUnchangedLeaves(pools, rec.PoolID, ids, cidrs); err != nil {
			return err
		}
		if err := rejectDuplicateCIDR(pools, rec.SuggestedCIDR); err != nil {
			return err
		}
		widened, found, err := ps.UpdatePoolCIDR(work, ids[0], rec.SuggestedCIDR)
		if err != nil {
			return fmt.Errorf("widen pool %d to %s: %w", ids[0], rec.SuggestedCIDR, err)
		}
		if !found {
			return fmt.Errorf("pool %d: %w", ids[0], storage.ErrNotFound)
		}
		deleted, err := ps.DeletePool(work, ids[1])
		if err != nil || !deleted {
			// Stores without a placement transaction keep the widened range unless it is put back.
			_, _, _ = ps.UpdatePoolCIDR(work, ids[0], cidrs[0])
			if err == nil {
				err = storage.ErrNotFound
			}
			return fmt.Errorf("delete pool %d: %w", ids[1], err)
		}
		kept = widened
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := s.relinkResources(ctx, ids[1], kept.ID); err != nil {
		return nil, err
	}
	if req.Name != "" || req.AccountID != nil {
		name := kept.Name
		if req.Name != "" {
			name = req.Name
		}
		accountID := kept.AccountID
		if req.AccountID != nil {
			accountID = req.AccountID
		}
		if _, _, err := s.mainStore.UpdatePoolMeta(ctx, kept.ID, &name, accountID); err != nil {
			return nil, fmt.Errorf("rename merged pool %d: %w", kept.ID, err)
		}
	}
	return &kept.ID, nil
}

// consolidationSources reads the two source pool IDs and CIDRs the recommendation recorded.
func consolidationSources(rec *domain.Recommendation) ([2]int64, [2]string, error) {
	var ids [2]int64
	var cidrs [2]string
	rawIDs := strings.Split(rec.Metadata["source_pool_ids"], ",")
	rawCIDRs := strings.Split(rec.Metadata["source_cidrs"], ",")
	if len(rawIDs) != 2 || len(rawCIDRs) != 2 {
		return ids, cidrs, fmt.Errorf("consolidation recommendation %s has no source pools: %w", rec.ID, storage.ErrValidation)
	}
	for i := range ids {
		id, err := strconv.ParseInt(rawIDs[i], 10, 64)
		if err != nil {
			return ids, cidrs, fmt.Errorf("consolidation recommendation %s has source pool id %q: %w", rec.ID, rawIDs[i], storage.ErrValidation)
		}
		ids[i], cidrs[i] = id, rawCIDRs[i]
	}
	return ids, cidrs, nil
}

// checkUnchangedLeaves confirms each source pool is still what the recommendation saw: live,
// under the same parent, with the same CIDR, and without children.
func checkUnchangedLeaves(pools []domain.Pool, parentID int64, ids [2]int64, cidrs [2]string) error {
	tree := newPoolTree(pools)
	byID := make(map[int64]domain.Pool, len(pools))
	for _, p := range pools {
		byID[p.ID] = p
	}
	for i, id := range ids {
		p, ok := byID[id]
		if !ok || p.ParentID == nil || *p.ParentID != parentID || p.CIDR != cidrs[i] {
			return fmt.Errorf("source pool %d changed since the recommendation was generated: %w", id, storage.ErrConflict)
		}
		if !tree.isLeaf(id) {
			return fmt.Errorf("source pool %d now has child pools: %w", id, storage.ErrConflict)
		}
	}
	return nil
}

// rejectDuplicateCIDR fails when a live pool anywhere already carries cidr, which PostgreSQL's
// organization-wide unique index would reject.
func rejectDuplicateCIDR(pools []domain.Pool, cidr string) error {
	for _, p := range pools {
		if p.CIDR == cidr {
			return fmt.Errorf("pool %d already uses %s: %w", p.ID, cidr, storage.ErrConflict)
		}
	}
	return nil
}
