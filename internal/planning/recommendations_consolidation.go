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

// applyConsolidation deletes the two source pools and places the merged block under the
// recommendation's pool through the placement guard. The three writes are not one
// transaction: when placement fails, the sources stay deleted and the recommendation stays
// pending, so the caller sees the error and the pools need to be recreated by hand.
func (s *RecommendationService) applyConsolidation(ctx context.Context, rec *domain.Recommendation, req domain.ApplyRecommendationRequest) (*int64, error) {
	sources, err := s.consolidationSources(ctx, rec)
	if err != nil {
		return nil, err
	}
	if err := s.rejectDuplicateCIDR(ctx, rec.SuggestedCIDR); err != nil {
		return nil, err
	}
	for _, src := range sources {
		if _, err := s.mainStore.DeletePool(ctx, src.ID); err != nil {
			return nil, fmt.Errorf("delete source pool %d: %w", src.ID, err)
		}
	}

	name := req.Name
	if name == "" {
		name = "Consolidated " + rec.SuggestedCIDR
	}
	accountID := req.AccountID
	if accountID == nil {
		accountID = sources[0].AccountID
	}
	parentID := rec.PoolID
	merged, err := s.placer.PlacePool(ctx, domain.CreatePool{
		Name:      name,
		CIDR:      rec.SuggestedCIDR,
		ParentID:  &parentID,
		AccountID: accountID,
		Type:      sources[0].Type,
		Status:    sources[0].Status,
		Source:    domain.PoolSourceManual,
	})
	if err != nil {
		return nil, fmt.Errorf("place merged pool %s after deleting its sources: %w", rec.SuggestedCIDR, err)
	}
	return &merged.ID, nil
}

// consolidationSources loads the two source pools and checks they are still what the
// recommendation saw: present, under the same parent, with the same CIDRs, and without children.
func (s *RecommendationService) consolidationSources(ctx context.Context, rec *domain.Recommendation) ([]domain.Pool, error) {
	ids := strings.Split(rec.Metadata["source_pool_ids"], ",")
	cidrs := strings.Split(rec.Metadata["source_cidrs"], ",")
	if len(ids) != 2 || len(cidrs) != 2 {
		return nil, fmt.Errorf("consolidation recommendation %s has no source pools: %w", rec.ID, storage.ErrValidation)
	}
	sources := make([]domain.Pool, 0, 2)
	for i, raw := range ids {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("consolidation recommendation %s has source pool id %q: %w", rec.ID, raw, storage.ErrValidation)
		}
		pool, found, err := s.mainStore.GetPool(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get source pool %d: %w", id, err)
		}
		if !found || pool.ParentID == nil || *pool.ParentID != rec.PoolID || pool.CIDR != cidrs[i] {
			return nil, fmt.Errorf("source pool %d changed since the recommendation was generated: %w", id, storage.ErrConflict)
		}
		children, err := s.mainStore.GetPoolChildren(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get children of source pool %d: %w", id, err)
		}
		if len(children) > 0 {
			return nil, fmt.Errorf("source pool %d now has child pools: %w", id, storage.ErrConflict)
		}
		sources = append(sources, pool)
	}
	return sources, nil
}

// rejectDuplicateCIDR fails before any source is deleted when a live pool anywhere already
// carries the merged CIDR, which PostgreSQL's unique index would reject after the deletes.
func (s *RecommendationService) rejectDuplicateCIDR(ctx context.Context, cidr string) error {
	pools, err := s.mainStore.ListPools(ctx)
	if err != nil {
		return fmt.Errorf("list pools: %w", err)
	}
	for _, p := range pools {
		if p.CIDR == cidr {
			return fmt.Errorf("pool %d already uses %s: %w", p.ID, cidr, storage.ErrConflict)
		}
	}
	return nil
}
