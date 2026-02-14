package planning

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// RecommendationService generates, applies, and dismisses recommendations.
type RecommendationService struct {
	analysis  *AnalysisService
	store     storage.RecommendationStore
	mainStore storage.Store
}

// NewRecommendationService creates a new RecommendationService.
func NewRecommendationService(analysis *AnalysisService, store storage.RecommendationStore, mainStore storage.Store) *RecommendationService {
	return &RecommendationService{
		analysis:  analysis,
		store:     store,
		mainStore: mainStore,
	}
}

// Generate creates recommendations for the given pools by running analysis.
func (s *RecommendationService) Generate(ctx context.Context, req domain.GenerateRecommendationsRequest) (*domain.GenerateRecommendationsResponse, error) {
	pools, err := s.analysis.resolvePools(ctx, req.PoolIDs, req.IncludeChildren)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var allRecs []domain.Recommendation

	for _, pool := range pools {
		// Clear stale pending recommendations before regeneration.
		if err := s.store.DeletePendingForPool(ctx, pool.ID); err != nil {
			return nil, fmt.Errorf("clear pending for pool %d: %w", pool.ID, err)
		}

		// Gap analysis → allocation recommendations.
		gap, err := s.analysis.AnalyzeGaps(ctx, pool.ID)
		if err == nil && gap != nil {
			for _, block := range gap.AvailableBlocks {
				score := s.scoreAllocation(block.CIDR, pool, req.DesiredPrefixLen)
				rec := domain.Recommendation{
					ID:            uuid.New().String(),
					PoolID:        pool.ID,
					Type:          domain.RecommendationTypeAllocation,
					Status:        domain.RecommendationStatusPending,
					Priority:      priorityFromScore(score),
					Title:         fmt.Sprintf("Allocate %s", block.CIDR),
					Description:   fmt.Sprintf("Available block %s (%d addresses) in pool %q", block.CIDR, block.AddressCount, pool.Name),
					SuggestedCIDR: block.CIDR,
					Score:         score,
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				if err := s.store.CreateRecommendation(ctx, rec); err != nil {
					return nil, fmt.Errorf("create allocation rec: %w", err)
				}
				allRecs = append(allRecs, rec)
			}
		}

		// Compliance → compliance recommendations.
		compliance, err := s.analysis.CheckCompliance(ctx, []int64{pool.ID}, false)
		if err == nil && compliance != nil {
			for _, v := range compliance.Violations {
				if v.PoolID != pool.ID {
					continue
				}
				rec := s.complianceRecommendation(v, pool, now)
				if err := s.store.CreateRecommendation(ctx, rec); err != nil {
					return nil, fmt.Errorf("create compliance rec: %w", err)
				}
				allRecs = append(allRecs, rec)
			}
		}
	}

	if allRecs == nil {
		allRecs = []domain.Recommendation{}
	}

	return &domain.GenerateRecommendationsResponse{
		Items: allRecs,
		Total: len(allRecs),
	}, nil
}

// Apply applies a recommendation: for allocation types, creates a new child pool.
func (s *RecommendationService) Apply(ctx context.Context, id string, req domain.ApplyRecommendationRequest) (*domain.Recommendation, error) {
	rec, err := s.store.GetRecommendation(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != domain.RecommendationStatusPending {
		return nil, fmt.Errorf("recommendation is %s, not pending: %w", rec.Status, storage.ErrConflict)
	}

	var appliedPoolID *int64

	switch rec.Type {
	case domain.RecommendationTypeAllocation:
		name := req.Name
		if name == "" {
			name = fmt.Sprintf("Allocation %s", rec.SuggestedCIDR)
		}
		parentID := rec.PoolID
		newPool, err := s.mainStore.CreatePool(ctx, domain.CreatePool{
			Name:     name,
			CIDR:     rec.SuggestedCIDR,
			ParentID: &parentID,
			AccountID: req.AccountID,
			Source:   domain.PoolSourceManual,
		})
		if err != nil {
			return nil, fmt.Errorf("create pool from recommendation: %w", err)
		}
		appliedPoolID = &newPool.ID

	case domain.RecommendationTypeCompliance:
		// Compliance fixes are manual; marking as applied acknowledges the issue.
	}

	if err := s.store.UpdateRecommendationStatus(ctx, id, domain.RecommendationStatusApplied, "", appliedPoolID); err != nil {
		return nil, err
	}

	rec.Status = domain.RecommendationStatusApplied
	rec.AppliedPoolID = appliedPoolID
	return rec, nil
}

// Dismiss dismisses a recommendation with an optional reason.
func (s *RecommendationService) Dismiss(ctx context.Context, id string, reason string) (*domain.Recommendation, error) {
	rec, err := s.store.GetRecommendation(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != domain.RecommendationStatusPending {
		return nil, fmt.Errorf("recommendation is %s, not pending: %w", rec.Status, storage.ErrConflict)
	}

	if err := s.store.UpdateRecommendationStatus(ctx, id, domain.RecommendationStatusDismissed, reason, nil); err != nil {
		return nil, err
	}

	rec.Status = domain.RecommendationStatusDismissed
	rec.DismissReason = reason
	return rec, nil
}

// scoreAllocation scores an available block (0-100) for allocation quality.
func (s *RecommendationService) scoreAllocation(cidrStr string, pool domain.Pool, desiredPrefixLen int) int {
	prefix, err := netip.ParsePrefix(cidrStr)
	if err != nil {
		return 0
	}
	prefix = prefix.Masked()

	score := 0

	// Alignment (+30): block starts on its natural boundary.
	addr := ipv4ToUint32(prefix.Addr())
	blockSize := uint32(1) << (32 - prefix.Bits())
	if addr%blockSize == 0 {
		score += 30
	}

	// Size fit (+30): prefer /24-/28 for workloads, or match desired prefix len.
	bits := prefix.Bits()
	if desiredPrefixLen > 0 {
		if bits == desiredPrefixLen {
			score += 30
		} else {
			diff := bits - desiredPrefixLen
			if diff < 0 {
				diff = -diff
			}
			if diff <= 2 {
				score += 15
			}
		}
	} else {
		if bits >= 24 && bits <= 28 {
			score += 30
		} else if bits >= 20 && bits <= 30 {
			score += 15
		}
	}

	// Contiguity (+20): adjacent to an existing child allocation.
	// Check by looking at pool's children.
	children, err := s.mainStore.GetPoolChildren(context.Background(), pool.ID)
	if err == nil {
		blockInterval := prefixToInterval(prefix)
		for _, child := range children {
			cp, err := netip.ParsePrefix(child.CIDR)
			if err != nil {
				continue
			}
			ci := prefixToInterval(cp.Masked())
			// Adjacent if blocks touch.
			if ci.end+1 == blockInterval.start || blockInterval.end+1 == ci.start {
				score += 20
				break
			}
		}
	}

	// RFC1918 (+20): within private address space.
	if isRFC1918(prefix) {
		score += 20
	}

	if score > 100 {
		score = 100
	}
	return score
}

// complianceRecommendation maps a compliance violation to a recommendation.
func (s *RecommendationService) complianceRecommendation(v ComplianceViolation, pool domain.Pool, now time.Time) domain.Recommendation {
	rec := domain.Recommendation{
		ID:        uuid.New().String(),
		PoolID:    pool.ID,
		Type:      domain.RecommendationTypeCompliance,
		Status:    domain.RecommendationStatusPending,
		RuleID:    v.RuleID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	switch v.RuleID {
	case "OVERLAP-001":
		rec.Priority = domain.RecommendationPriorityHigh
		rec.Score = 90
		rec.Title = "Resolve CIDR overlap"
		rec.Description = v.Message
	case "RFC1918-001":
		rec.Priority = domain.RecommendationPriorityMedium
		rec.Score = 60
		rec.Title = "Move to RFC1918 space"
		rec.Description = v.Message
	case "EMPTY-001":
		rec.Priority = domain.RecommendationPriorityLow
		rec.Score = 30
		rec.Title = "Add allocations or reclassify"
		rec.Description = v.Message
	case "NAME-001":
		rec.Priority = domain.RecommendationPriorityLow
		rec.Score = 20
		rec.Title = "Add pool name"
		rec.Description = v.Message
	case "NAME-002":
		rec.Priority = domain.RecommendationPriorityLow
		rec.Score = 20
		rec.Title = "Add pool description"
		rec.Description = v.Message
	default:
		rec.Priority = domain.RecommendationPriorityMedium
		rec.Score = 50
		rec.Title = fmt.Sprintf("Fix compliance issue: %s", v.RuleID)
		rec.Description = v.Message
	}

	return rec
}

// priorityFromScore derives priority from a numeric score.
func priorityFromScore(score int) domain.RecommendationPriority {
	switch {
	case score >= 70:
		return domain.RecommendationPriorityHigh
	case score >= 40:
		return domain.RecommendationPriorityMedium
	default:
		return domain.RecommendationPriorityLow
	}
}
