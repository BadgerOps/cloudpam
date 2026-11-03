package planning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// A consolidation name goes through the same validator as POST and PATCH /pools, and a bad one is
// refused before either source pool changes.
func TestApply_ConsolidationRefusesInvalidName(t *testing.T) {
	for name, bad := range map[string]string{
		"blank":   "   ",
		"control": "bad\x00name",
		"long":    strings.Repeat("n", 300),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, st, _ := setupTypedRecService(t)
			parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
			a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
			b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
			rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]

			if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{Name: bad}); !errors.Is(err, storage.ErrValidation) {
				t.Fatalf("apply with name %q: err=%v, want ErrValidation", bad, err)
			}
			for _, src := range []domain.Pool{a, b} {
				if got, found, _ := st.GetPool(ctx, src.ID); !found || got.CIDR != src.CIDR || got.Name != src.Name {
					t.Errorf("source pool %d is %+v after a refused apply, want it unchanged", src.ID, got)
				}
			}
		})
	}
}

// A supplied name is stored trimmed, as PATCH /pools stores it.
func TestApply_ConsolidationTrimsName(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{Name: "  merged  "}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if kept, _, _ := st.GetPool(ctx, a.ID); kept.Name != "merged" {
		t.Errorf("kept pool name %q, want merged", kept.Name)
	}
}

// flakyStatusStore fails the next failures status writes, after the apply's pool change committed.
type flakyStatusStore struct {
	storage.RecommendationStore
	failures *int
}

func (f flakyStatusStore) UpdateRecommendationStatus(ctx context.Context, id string, status domain.RecommendationStatus, reason string, poolID *int64) error {
	if *f.failures > 0 {
		*f.failures--
		return errors.New("recommendation store unavailable")
	}
	return f.RecommendationStore.UpdateRecommendationStatus(ctx, id, status, reason, poolID)
}

// When the resize is stored but recording the status fails, applying again finds the pool already
// at the target block and records success instead of refusing the finished resize.
func TestApply_ResizeRetryRecordsAnAlreadyStoredResize(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	failures := 1
	recStore := flakyStatusStore{storage.NewMemoryRecommendationStore(st), &failures}
	svc := NewRecommendationService(NewAnalysisService(st), recStore, st, storage.NewMemoryDiscoveryStore(st), storePlacer{st})
	pool := mustPool(t, st, "Sparse", "10.0.0.0/16", nil, nil)
	mustPool(t, st, "a", "10.0.0.0/24", &pool.ID, nil)
	mustPool(t, st, "b", "10.0.1.0/24", &pool.ID, nil)
	rec := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeResize)[0]

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); err == nil {
		t.Fatal("apply whose status write failed reported success")
	}
	if got, _, _ := st.GetPool(ctx, pool.ID); got.CIDR != rec.SuggestedCIDR {
		t.Fatalf("pool %s after the first apply, want the stored resize %s", got.CIDR, rec.SuggestedCIDR)
	}

	applied, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("retry of a stored resize: %v", err)
	}
	if applied.Status != domain.RecommendationStatusApplied || applied.AppliedPoolID == nil || *applied.AppliedPoolID != pool.ID {
		t.Fatalf("retry recorded %+v, want applied to pool %d", applied, pool.ID)
	}
}

// When the reclaim's delete is stored but recording the status fails, applying again finds the pool
// gone, which is what the recommendation asks for, and records success.
func TestApply_ReclaimRetryRecordsAnAlreadyStoredDelete(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	failures := 1
	recStore := flakyStatusStore{storage.NewMemoryRecommendationStore(st), &failures}
	svc := NewRecommendationService(NewAnalysisService(st), recStore, st, storage.NewMemoryDiscoveryStore(st), storePlacer{st})
	pool := mustPool(t, st, "Idle", "10.0.5.0/24", nil, nil)
	rec := reclaimRecFor(t, svc, pool.ID)

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); err == nil {
		t.Fatal("apply whose status write failed reported success")
	}
	if _, found, _ := st.GetPool(ctx, pool.ID); found {
		t.Fatalf("pool %d is live after the first apply, want the stored delete", pool.ID)
	}

	applied, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("retry of a stored reclaim: %v", err)
	}
	if applied.Status != domain.RecommendationStatusApplied {
		t.Fatalf("retry recorded %s, want applied", applied.Status)
	}
}
