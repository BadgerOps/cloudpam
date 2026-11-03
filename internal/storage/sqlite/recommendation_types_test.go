//go:build sqlite

package sqlite

import (
	"context"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

type storePlacer struct{ s *Store }

func (p storePlacer) PlacePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	return p.s.CreatePool(ctx, in)
}

func (p storePlacer) WithPlacement(ctx context.Context, fn func(context.Context, storage.PlacementStore) error) error {
	return fn(ctx, p.s)
}

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

func TestConsolidationResizeAndReclaimRoundTripThroughSQLite(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	svc := planning.NewRecommendationService(planning.NewAnalysisService(s), s, s, s, storePlacer{s})

	create := func(name, cidr string, parentID *int64) domain.Pool {
		t.Helper()
		p, err := s.CreatePool(ctx, domain.CreatePool{Name: name, CIDR: cidr, ParentID: parentID})
		if err != nil {
			t.Fatalf("create pool %s: %v", cidr, err)
		}
		return p
	}
	corp := create("corp", "10.0.0.0/16", nil)
	create("a", "10.0.1.0/25", &corp.ID)
	create("b", "10.0.1.128/25", &corp.ID)
	sparse := create("sparse", "10.1.0.0/16", nil)
	create("c", "10.1.0.0/24", &sparse.ID)
	idle := create("idle", "10.2.0.0/24", nil)
	backdated := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE pools SET updated_at=? WHERE id=?`, backdated, idle.ID); err != nil {
		t.Fatal(err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{corp.ID, sparse.ID, idle.ID}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	consolidation := recommendationOfType(t, resp.Items, domain.RecommendationTypeConsolidation)
	reclaim := recommendationOfType(t, resp.Items, domain.RecommendationTypeReclaim)
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

	if _, err := svc.Apply(ctx, reclaim.ID, domain.ApplyRecommendationRequest{}); err != nil {
		t.Fatalf("apply reclaim: %v", err)
	}
	if _, found, _ := s.GetPool(ctx, idle.ID); found {
		t.Error("idle pool still exists after reclaim")
	}

	for _, id := range []string{consolidation.ID, resize.ID, reclaim.ID} {
		rec, err := s.GetRecommendation(ctx, id)
		if err != nil || rec.Status != domain.RecommendationStatusApplied {
			t.Errorf("rec %s: %+v err=%v, want applied", id, rec, err)
		}
	}
}
