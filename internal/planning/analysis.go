package planning

import (
	"context"
	"fmt"
	"time"

	"cloudpam/internal/storage"
)

// AnalysisService provides network analysis capabilities.
type AnalysisService struct {
	store storage.Store
}

// NewAnalysisService creates a new AnalysisService.
func NewAnalysisService(store storage.Store) *AnalysisService {
	return &AnalysisService{store: store}
}

// Analyze runs gap analysis, fragmentation, and compliance checks across
// the requested pools and produces a combined report.
func (s *AnalysisService) Analyze(ctx context.Context, req AnalysisRequest) (*NetworkAnalysisReport, error) {
	pools, err := s.resolvePools(ctx, req.PoolIDs, req.IncludeChildren)
	if err != nil {
		return nil, err
	}

	if len(pools) == 0 {
		return &NetworkAnalysisReport{
			GeneratedAt: time.Now().UTC(),
			Summary: AnalysisSummary{
				HealthScore: 100,
			},
		}, nil
	}

	// Determine which pools are "root" (either requested, or have no parent).
	rootIDs := req.PoolIDs
	if len(rootIDs) == 0 {
		for _, p := range pools {
			if p.ParentID == nil {
				rootIDs = append(rootIDs, p.ID)
			}
		}
	}

	// Gap analysis per root pool.
	var gapAnalyses []GapAnalysis
	var totalAddr, usedAddr uint64
	for _, id := range rootIDs {
		gap, err := s.AnalyzeGaps(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("gap analysis for pool %d: %w", id, err)
		}
		gapAnalyses = append(gapAnalyses, *gap)
		totalAddr += gap.TotalAddresses
		usedAddr += gap.UsedAddresses
	}

	// Fragmentation for the first root pool (or aggregate if multiple).
	var fragResult *FragmentationAnalysis
	if len(rootIDs) > 0 {
		frag, err := s.AnalyzeFragmentation(ctx, rootIDs[0])
		if err == nil {
			fragResult = frag
		}
	}

	// Compliance across all resolved pools.
	allIDs := make([]int64, len(pools))
	for i, p := range pools {
		allIDs[i] = p.ID
	}
	compliance, err := s.CheckCompliance(ctx, allIDs, false)
	if err != nil {
		return nil, fmt.Errorf("compliance check: %w", err)
	}

	// Compute health score: start at 100, deduct for issues.
	healthScore := 100
	var errorCount, warningCount, infoCount int
	if compliance != nil {
		for _, v := range compliance.Violations {
			switch v.Severity {
			case "error":
				errorCount++
				healthScore -= 15
			case "warning":
				warningCount++
				healthScore -= 5
			case "info":
				infoCount++
				healthScore -= 1
			}
		}
	}
	if fragResult != nil {
		// Deduct based on fragmentation score.
		healthScore -= fragResult.Score / 5
	}
	if healthScore < 0 {
		healthScore = 0
	}

	var util float64
	if totalAddr > 0 {
		util = float64(usedAddr) / float64(totalAddr) * 100
	}

	return &NetworkAnalysisReport{
		GeneratedAt: time.Now().UTC(),
		Summary: AnalysisSummary{
			TotalPools:         len(pools),
			TotalAddresses:     totalAddr,
			UsedAddresses:      usedAddr,
			AvailableAddresses: totalAddr - usedAddr,
			Utilization:        util,
			HealthScore:        healthScore,
			ErrorCount:         errorCount,
			WarningCount:       warningCount,
			InfoCount:          infoCount,
		},
		GapAnalyses:   gapAnalyses,
		Fragmentation: fragResult,
		Compliance:    compliance,
	}, nil
}

// Store returns the underlying store (used by HTTP handlers for pool lookups).
func (s *AnalysisService) Store() storage.Store {
	return s.store
}
