//go:build postgres

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
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

// failingWidenPlacer runs the placement in PostgreSQL's transaction but fails the widen, after
// consolidation has already deleted the absorbed pool inside that transaction, and fails the undo.
type failingWidenPlacer struct{ lockedPlacer }

type failingWidenStore struct{ storage.PlacementStore }

func (failingWidenStore) UpdatePoolCIDR(context.Context, int64, string) (domain.Pool, bool, error) {
	return domain.Pool{}, false, errors.New("widen failed")
}

// RestorePool fails too, so only the transaction's rollback can bring the absorbed pool back.
func (failingWidenStore) RestorePool(context.Context, int64) (domain.Pool, bool, error) {
	return domain.Pool{}, false, errors.New("restore failed")
}

func (p failingWidenPlacer) WithPlacement(ctx context.Context, fn func(context.Context, storage.PlacementStore) error) error {
	return p.lockedPlacer.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		return fn(work, failingWidenStore{ps})
	})
}

// On PostgreSQL the delete and the widen share one transaction, so a failed widen rolls the
// delete back without any undo step.
func TestConsolidationFailedWidenRollsBackTheDeleteOnPostgres(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	corp := mustCreatePool(t, s, "corp", "10.0.0.0/16", nil)
	a := mustCreatePool(t, s, "a", "10.0.1.0/25", &corp.ID)
	b := mustCreatePool(t, s, "b", "10.0.1.128/25", &corp.ID)

	gen := planning.NewRecommendationService(planning.NewAnalysisService(s), s, s, s, lockedPlacer{s})
	resp, err := gen.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{corp.ID}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	rec := recommendationOfType(t, resp.Items, domain.RecommendationTypeConsolidation)

	failing := planning.NewRecommendationService(planning.NewAnalysisService(s), s, s, s, failingWidenPlacer{lockedPlacer{s}})
	if _, err := failing.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); err == nil {
		t.Fatal("apply with a failing widen reported success")
	}
	for _, p := range []domain.Pool{a, b} {
		got, found, err := s.GetPool(ctx, p.ID)
		if err != nil || !found || got.CIDR != p.CIDR {
			t.Fatalf("pool %d after the rolled-back apply = %+v found=%v err=%v, want it live at %s", p.ID, got, found, err, p.CIDR)
		}
	}
}
