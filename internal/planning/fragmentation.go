package planning

import (
	"context"
	"fmt"
	"net/netip"

	"cloudpam/internal/storage"
)

// AnalyzeFragmentation scores the fragmentation of a pool's address space.
// Score ranges from 0 (perfectly organized) to 100 (severely fragmented).
func (s *AnalysisService) AnalyzeFragmentation(ctx context.Context, poolID int64) (*FragmentationAnalysis, error) {
	pool, found, err := s.store.GetPool(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("get pool %d: %w", poolID, err)
	}
	if !found {
		return nil, fmt.Errorf("pool %d: %w", poolID, storage.ErrNotFound)
	}

	children, err := s.store.GetPoolChildren(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("get children for pool %d: %w", poolID, err)
	}

	result := &FragmentationAnalysis{
		PoolID:   poolID,
		PoolName: pool.Name,
	}

	if len(children) == 0 {
		// No children means no fragmentation to analyze.
		return result, nil
	}

	// Run gap analysis to get gap count.
	gaps, err := s.AnalyzeGaps(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("gap analysis for fragmentation: %w", err)
	}

	var (
		scatteredScore  float64
		oversizedScore  float64
		undersizedScore float64
		misalignedScore float64
	)

	// Scattered factor (40% weight): ratio of gaps to total segments.
	gapCount := len(gaps.AvailableBlocks)
	childCount := len(children)
	totalSegments := childCount + gapCount
	if totalSegments > 0 {
		scatteredScore = float64(gapCount) / float64(totalSegments)
	}
	if gapCount > 1 {
		result.Issues = append(result.Issues, FragmentationIssue{
			Type:        FragmentScattered,
			Severity:    severityForScore(scatteredScore),
			CIDR:        pool.CIDR,
			PoolID:      poolID,
			Description: fmt.Sprintf("address space has %d gaps among %d children", gapCount, childCount),
		})
	}

	// Oversized factor (20% weight): children with utilization < 25%.
	var oversizedCount int
	for _, child := range children {
		stats, err := s.store.CalculatePoolUtilization(ctx, child.ID)
		if err != nil || stats == nil {
			continue
		}
		if stats.Utilization < 25 && stats.ChildCount == 0 {
			oversizedCount++
			result.Issues = append(result.Issues, FragmentationIssue{
				Type:        FragmentOversized,
				Severity:    "warning",
				CIDR:        child.CIDR,
				PoolID:      child.ID,
				Description: fmt.Sprintf("pool %q has low utilization (%.1f%%)", child.Name, stats.Utilization),
			})
		}
	}
	if childCount > 0 {
		oversizedScore = float64(oversizedCount) / float64(childCount)
	}

	// Undersized factor (20% weight): children with utilization > 90%.
	var undersizedCount int
	for _, child := range children {
		stats, err := s.store.CalculatePoolUtilization(ctx, child.ID)
		if err != nil || stats == nil {
			continue
		}
		if stats.Utilization > 90 {
			undersizedCount++
			result.Issues = append(result.Issues, FragmentationIssue{
				Type:        FragmentUndersized,
				Severity:    "warning",
				CIDR:        child.CIDR,
				PoolID:      child.ID,
				Description: fmt.Sprintf("pool %q is nearly full (%.1f%%)", child.Name, stats.Utilization),
			})
		}
	}
	if childCount > 0 {
		undersizedScore = float64(undersizedCount) / float64(childCount)
	}

	// Misaligned factor (20% weight): inconsistent prefix lengths among siblings.
	prefixLens := map[int]int{} // prefix length → count
	for _, child := range children {
		p, err := netip.ParsePrefix(child.CIDR)
		if err != nil {
			continue
		}
		prefixLens[p.Bits()]++
	}
	if len(prefixLens) > 1 {
		// More distinct prefix lengths → higher misalignment.
		misalignedScore = 1.0 - 1.0/float64(len(prefixLens))
		result.Issues = append(result.Issues, FragmentationIssue{
			Type:        FragmentMisaligned,
			Severity:    severityForScore(misalignedScore),
			CIDR:        pool.CIDR,
			PoolID:      poolID,
			Description: fmt.Sprintf("children use %d different prefix lengths", len(prefixLens)),
		})
	}

	// Weighted score.
	score := scatteredScore*40 + oversizedScore*20 + undersizedScore*20 + misalignedScore*20
	if score > 100 {
		score = 100
	}
	result.Score = int(score)

	// Recommendations.
	if scatteredScore > 0.3 {
		result.Recommendations = append(result.Recommendations,
			"Consider consolidating allocations to reduce gaps in the address space")
	}
	if oversizedCount > 0 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("Review %d oversized pools with <25%% utilization for right-sizing", oversizedCount))
	}
	if undersizedCount > 0 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("Plan capacity expansion for %d pools with >90%% utilization", undersizedCount))
	}
	if len(prefixLens) > 2 {
		result.Recommendations = append(result.Recommendations,
			"Standardize subnet sizes to reduce addressing complexity")
	}

	return result, nil
}

func severityForScore(s float64) string {
	switch {
	case s >= 0.7:
		return "error"
	case s >= 0.4:
		return "warning"
	default:
		return "info"
	}
}
