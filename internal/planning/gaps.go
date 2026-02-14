package planning

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	"cloudpam/internal/storage"
)

// AnalyzeGaps finds unused address space within a pool by comparing
// its CIDR range against its direct children.
func (s *AnalysisService) AnalyzeGaps(ctx context.Context, poolID int64) (*GapAnalysis, error) {
	pool, found, err := s.store.GetPool(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("get pool %d: %w", poolID, err)
	}
	if !found {
		return nil, fmt.Errorf("pool %d: %w", poolID, storage.ErrNotFound)
	}

	parent, err := netip.ParsePrefix(pool.CIDR)
	if err != nil {
		return nil, fmt.Errorf("parse pool CIDR %q: %w", pool.CIDR, err)
	}
	parent = parent.Masked()

	children, err := s.store.GetPoolChildren(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("get children for pool %d: %w", poolID, err)
	}

	// Build allocated blocks and child intervals.
	var childIntervals []interval
	var allocated []AllocatedBlock
	for _, child := range children {
		cp, err := netip.ParsePrefix(child.CIDR)
		if err != nil {
			continue
		}
		cp = cp.Masked()
		childIntervals = append(childIntervals, prefixToInterval(cp))

		var util float64
		if stats, err := s.store.CalculatePoolUtilization(ctx, child.ID); err == nil && stats != nil {
			util = stats.Utilization
		}
		allocated = append(allocated, AllocatedBlock{
			PoolID:      child.ID,
			Name:        child.Name,
			CIDR:        child.CIDR,
			Utilization: util,
		})
	}

	parentIv := prefixToInterval(parent)
	freeRanges := findFreeRanges(parentIv.start, parentIv.end, childIntervals)

	var available []AvailableBlock
	var freeAddrs uint64
	for _, fr := range freeRanges {
		cidrs := rangeToCIDRs(fr.start, fr.end)
		for _, c := range cidrs {
			count := prefixAddressCount(c)
			available = append(available, AvailableBlock{
				CIDR:         c.String(),
				AddressCount: count,
			})
			freeAddrs += count
		}
	}

	totalAddrs := prefixAddressCount(parent)
	usedAddrs := totalAddrs - freeAddrs
	var util float64
	if totalAddrs > 0 {
		util = float64(usedAddrs) / float64(totalAddrs) * 100
	}

	return &GapAnalysis{
		PoolID:          poolID,
		PoolName:        pool.Name,
		ParentCIDR:      pool.CIDR,
		AllocatedBlocks: allocated,
		AvailableBlocks: available,
		TotalAddresses:  totalAddrs,
		UsedAddresses:   usedAddrs,
		FreeAddresses:   freeAddrs,
		Utilization:     util,
	}, nil
}

// findFreeRanges returns the gaps in [parentStart, parentEnd] not covered
// by any child interval. Children may overlap; they are merged first.
func findFreeRanges(parentStart, parentEnd uint32, children []interval) []interval {
	if len(children) == 0 {
		return []interval{{start: parentStart, end: parentEnd}}
	}

	// Sort by start address.
	sort.Slice(children, func(i, j int) bool {
		return children[i].start < children[j].start
	})

	// Merge overlapping/adjacent intervals.
	merged := []interval{children[0]}
	for _, c := range children[1:] {
		last := &merged[len(merged)-1]
		if c.start <= last.end+1 {
			if c.end > last.end {
				last.end = c.end
			}
		} else {
			merged = append(merged, c)
		}
	}

	// Walk parent range, collecting gaps.
	var gaps []interval
	cursor := parentStart
	for _, m := range merged {
		// Clamp to parent bounds.
		mStart := m.start
		mEnd := m.end
		if mEnd < parentStart || mStart > parentEnd {
			continue
		}
		if mStart < parentStart {
			mStart = parentStart
		}
		if mEnd > parentEnd {
			mEnd = parentEnd
		}
		if cursor < mStart {
			gaps = append(gaps, interval{start: cursor, end: mStart - 1})
		}
		cursor = mEnd + 1
		if cursor == 0 { // uint32 overflow
			break
		}
	}
	if cursor != 0 && cursor <= parentEnd {
		gaps = append(gaps, interval{start: cursor, end: parentEnd})
	}

	return gaps
}
