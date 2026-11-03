package planning

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// expiringPlacer stands for a store without a placement transaction (SQLite, memory) whose
// placement budget runs out right after the first write: each write honors the context it is
// given, and the work context is cancelled once one write has been stored.
type expiringPlacer struct {
	storePlacer
	failRestore bool
}

type expiringPlacementStore struct {
	storage.PlacementStore
	expire      func()
	failRestore bool
}

func (e expiringPlacementStore) wrote() { e.expire() }

func (e expiringPlacementStore) UpdatePoolCIDR(ctx context.Context, id int64, cidr string) (domain.Pool, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.Pool{}, false, err
	}
	defer e.wrote()
	return e.PlacementStore.UpdatePoolCIDR(ctx, id, cidr)
}

func (e expiringPlacementStore) DeletePool(ctx context.Context, id int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	defer e.wrote()
	return e.PlacementStore.DeletePool(ctx, id)
}

func (e expiringPlacementStore) RestorePool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.Pool{}, false, err
	}
	if e.failRestore {
		return domain.Pool{}, false, errors.New("store unavailable")
	}
	return e.PlacementStore.RestorePool(ctx, id)
}

func (p expiringPlacer) WithPlacement(ctx context.Context, fn func(context.Context, storage.PlacementStore) error) error {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	return fn(work, expiringPlacementStore{PlacementStore: p.Store, expire: cancel, failRestore: p.failRestore})
}

func liveOverlappingSiblings(t *testing.T, st storage.Store, parentID int64) [][2]domain.Pool {
	t.Helper()
	children, err := st.GetPoolChildren(context.Background(), parentID)
	if err != nil {
		t.Fatal(err)
	}
	var out [][2]domain.Pool
	for i := range children {
		for j := i + 1; j < len(children); j++ {
			a, b := netip.MustParsePrefix(children[i].CIDR), netip.MustParsePrefix(children[j].CIDR)
			if a.Overlaps(b) {
				out = append(out, [2]domain.Pool{children[i], children[j]})
			}
		}
	}
	return out
}

// A consolidation whose budget ends between its two writes must not leave two live overlapping
// siblings, and applying it again must either finish the merge or be refused.
func TestApply_ConsolidationInterruptedBetweenWritesLeavesNoOverlap(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	recStore := storage.NewMemoryRecommendationStore(st)
	disc := storage.NewMemoryDiscoveryStore(st)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)

	generator := NewRecommendationService(NewAnalysisService(st), recStore, st, disc, storePlacer{st})
	recs := recsOfType(generateFor(t, generator, parent.ID), domain.RecommendationTypeConsolidation)
	if len(recs) != 1 {
		t.Fatalf("got %d consolidation recs, want 1", len(recs))
	}

	interrupted := NewRecommendationService(NewAnalysisService(st), recStore, st, disc, expiringPlacer{storePlacer: storePlacer{st}})
	if _, err := interrupted.Apply(ctx, recs[0].ID, domain.ApplyRecommendationRequest{}); err == nil {
		t.Fatal("apply whose budget ended between its writes reported success")
	}
	if overlaps := liveOverlappingSiblings(t, st, parent.ID); len(overlaps) > 0 {
		t.Fatalf("interrupted consolidation left live overlapping siblings %+v", overlaps)
	}

	applied, err := generator.Apply(ctx, recs[0].ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("retry after the interrupted apply: %v", err)
	}
	kept, found, _ := st.GetPool(ctx, a.ID)
	if !found || kept.CIDR != "10.0.1.0/24" || applied.AppliedPoolID == nil || *applied.AppliedPoolID != a.ID {
		t.Fatalf("retry kept %+v (applied %+v), want pool %d widened to 10.0.1.0/24", kept, applied, a.ID)
	}
	if _, found, _ := st.GetPool(ctx, b.ID); found {
		t.Fatalf("absorbed pool %d is still live after the retry", b.ID)
	}
	if overlaps := liveOverlappingSiblings(t, st, parent.ID); len(overlaps) > 0 {
		t.Fatalf("retry left live overlapping siblings %+v", overlaps)
	}
}

// When putting the absorbed pool back fails too, the pools still do not overlap, and applying the
// recommendation again is refused instead of widening into a range it no longer checked.
func TestApply_ConsolidationInterruptedWithFailedUndoIsRefusedOnRetry(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	recStore := storage.NewMemoryRecommendationStore(st)
	disc := storage.NewMemoryDiscoveryStore(st)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)

	generator := NewRecommendationService(NewAnalysisService(st), recStore, st, disc, storePlacer{st})
	recs := recsOfType(generateFor(t, generator, parent.ID), domain.RecommendationTypeConsolidation)
	if len(recs) != 1 {
		t.Fatalf("got %d consolidation recs, want 1", len(recs))
	}

	interrupted := NewRecommendationService(NewAnalysisService(st), recStore, st, disc, expiringPlacer{storePlacer: storePlacer{st}, failRestore: true})
	if _, err := interrupted.Apply(ctx, recs[0].ID, domain.ApplyRecommendationRequest{}); err == nil {
		t.Fatal("apply whose budget ended between its writes reported success")
	}
	if overlaps := liveOverlappingSiblings(t, st, parent.ID); len(overlaps) > 0 {
		t.Fatalf("interrupted consolidation left live overlapping siblings %+v", overlaps)
	}

	_, err := generator.Apply(ctx, recs[0].ID, domain.ApplyRecommendationRequest{})
	if !errors.Is(err, storage.ErrConflict) && !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("retry after a failed undo = %v, want a conflict or not-found refusal", err)
	}
	kept, _, _ := st.GetPool(ctx, a.ID)
	if kept.CIDR != a.CIDR {
		t.Fatalf("refused retry changed pool %d to %s", a.ID, kept.CIDR)
	}
	if rec, _ := recStore.GetRecommendation(ctx, recs[0].ID); rec.Status != domain.RecommendationStatusPending {
		t.Fatalf("refused retry left the recommendation %s, want pending", rec.Status)
	}
}
