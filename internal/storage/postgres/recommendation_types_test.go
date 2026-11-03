//go:build postgres

package postgres

import (
	"context"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
)

func recommendationOfType(t *testing.T, items []domain.Recommendation, typ domain.RecommendationType) domain.Recommendation {
	t.Helper()
	for _, rec := range items {
		if rec.Type == typ {
			return rec
		}
	}
	t.Fatalf("no %s recommendation in %+v", typ, items)
	return domain.Recommendation{}
}

// Reclaim is not covered here: the pools updated_at trigger makes a pool impossible to
// backdate without changing the shared database's schema.
func TestConsolidationAndResizeRoundTripThroughPostgres(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	svc := planning.NewRecommendationService(planning.NewAnalysisService(s), s, s, s, lockedPlacer{s})

	corp := mustCreatePool(t, s, "corp", "10.0.0.0/16", nil)
	mustCreatePool(t, s, "a", "10.0.1.0/25", &corp.ID)
	mustCreatePool(t, s, "b", "10.0.1.128/25", &corp.ID)
	sparse := mustCreatePool(t, s, "sparse", "10.1.0.0/16", nil)
	mustCreatePool(t, s, "c", "10.1.0.0/24", &sparse.ID)

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{corp.ID, sparse.ID}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	consolidation := recommendationOfType(t, resp.Items, domain.RecommendationTypeConsolidation)
	var resize domain.Recommendation
	for _, rec := range resp.Items {
		if rec.Type == domain.RecommendationTypeResize && rec.PoolID == sparse.ID {
			resize = rec
		}
	}
	if resize.ID == "" {
		t.Fatalf("no resize recommendation for sparse in %+v", resp.Items)
	}

	stored, err := s.GetRecommendation(ctx, consolidation.ID)
	if err != nil {
		t.Fatalf("get consolidation: %v", err)
	}
	if stored.Metadata["source_pool_ids"] != consolidation.Metadata["source_pool_ids"] {
		t.Errorf("metadata did not survive storage: %+v", stored.Metadata)
	}

	applied, err := svc.Apply(ctx, consolidation.ID, domain.ApplyRecommendationRequest{Name: "merged"})
	if err != nil {
		t.Fatalf("apply consolidation: %v", err)
	}
	merged, found, _ := s.GetPool(ctx, *applied.AppliedPoolID)
	if !found || merged.CIDR != "10.0.1.0/24" || merged.Name != "merged" {
		t.Errorf("merged pool %+v, want 10.0.1.0/24 named merged", merged)
	}
	if children, _ := s.GetPoolChildren(ctx, corp.ID); len(children) != 1 {
		t.Errorf("corp has %d children after the merge, want 1", len(children))
	}

	if _, err := svc.Apply(ctx, resize.ID, domain.ApplyRecommendationRequest{}); err != nil {
		t.Fatalf("apply resize: %v", err)
	}
	if got, _, _ := s.GetPool(ctx, sparse.ID); got.CIDR != "10.1.0.0/23" {
		t.Errorf("sparse cidr %q after resize, want 10.1.0.0/23", got.CIDR)
	}

	for _, id := range []string{consolidation.ID, resize.ID} {
		rec, err := s.GetRecommendation(ctx, id)
		if err != nil || rec.Status != domain.RecommendationStatusApplied || rec.UpdatedAt.Before(time.Now().Add(-time.Minute)) {
			t.Errorf("rec %s: %+v err=%v, want applied just now", id, rec, err)
		}
	}
}
