package planning

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// guardedPlacer runs fn against a PlacementStore that records the pools deleted through it, so a
// test can tell a delete made under the placement guard from one made around it.
type guardedPlacer struct {
	storePlacer
	deleted *[]int64
}

type recordingPlacementStore struct {
	storage.PlacementStore
	deleted *[]int64
}

func (r recordingPlacementStore) DeletePool(ctx context.Context, id int64) (bool, error) {
	*r.deleted = append(*r.deleted, id)
	return r.PlacementStore.DeletePool(ctx, id)
}

func (p guardedPlacer) WithPlacement(ctx context.Context, fn func(context.Context, storage.PlacementStore) error) error {
	return fn(ctx, recordingPlacementStore{PlacementStore: p.Store, deleted: p.deleted})
}

// flakyLinks fails the next failures calls to LinkResourceToPool.
type flakyLinks struct {
	storage.DiscoveryStore
	failures *int
}

func (f flakyLinks) LinkResourceToPool(ctx context.Context, resourceID uuid.UUID, poolID int64) error {
	if *f.failures > 0 {
		*f.failures--
		return errors.New("discovery store unavailable")
	}
	return f.DiscoveryStore.LinkResourceToPool(ctx, resourceID, poolID)
}

func reclaimRecFor(t *testing.T, svc *RecommendationService, poolID int64) domain.Recommendation {
	t.Helper()
	base := time.Now()
	svc.now = func() time.Time { return base.Add(45 * 24 * time.Hour) }
	recs := recsOfType(generateFor(t, svc, poolID), domain.RecommendationTypeReclaim)
	if len(recs) != 1 {
		t.Fatalf("got %d reclaim recs, want 1", len(recs))
	}
	return recs[0]
}

func TestApply_ReclaimDeletesUnderPlacementGuard(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	var deleted []int64
	svc := NewRecommendationService(NewAnalysisService(st), storage.NewMemoryRecommendationStore(st), st,
		storage.NewMemoryDiscoveryStore(st), guardedPlacer{storePlacer{st}, &deleted})
	pool := mustPool(t, st, "Idle", "10.0.5.0/24", nil, nil)
	rec := reclaimRecFor(t, svc, pool.ID)

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !slices.Equal(deleted, []int64{pool.ID}) {
		t.Errorf("pools deleted under the placement guard %v, want [%d]", deleted, pool.ID)
	}
}

func TestApply_ReclaimRefusesChangedPool(t *testing.T) {
	ctx := context.Background()
	other := "reassigned"
	cases := map[string]func(t *testing.T, st *storage.MemoryStore, id int64){
		"renamed": func(t *testing.T, st *storage.MemoryStore, id int64) {
			if _, _, err := st.UpdatePoolMeta(ctx, id, &other, nil); err != nil {
				t.Fatal(err)
			}
		},
		"resized": func(t *testing.T, st *storage.MemoryStore, id int64) {
			if _, _, err := st.UpdatePoolCIDR(ctx, id, "10.0.5.0/25"); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			svc, st, _ := setupTypedRecService(t)
			pool := mustPool(t, st, "Idle", "10.0.5.0/24", nil, nil)
			rec := reclaimRecFor(t, svc, pool.ID)
			change(t, st, pool.ID)

			if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); !errors.Is(err, storage.ErrConflict) {
				t.Fatalf("apply after the pool changed: err=%v, want ErrConflict", err)
			}
			if _, found, _ := st.GetPool(ctx, pool.ID); !found {
				t.Error("pool was reclaimed although it changed after the recommendation")
			}
		})
	}
}

func TestApply_ConsolidationRefusesReassignedSource(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	acct, err := st.CreateAccount(ctx, domain.CreateAccount{Key: "aws:2", Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]
	if _, _, err := st.UpdatePoolMeta(ctx, b.ID, nil, &acct.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("apply after a source moved accounts: err=%v, want ErrConflict", err)
	}
	for _, src := range []domain.Pool{a, b} {
		if got, found, _ := st.GetPool(ctx, src.ID); !found || got.CIDR != src.CIDR {
			t.Errorf("source pool %d is %+v after a refused apply, want %s unchanged", src.ID, got, src.CIDR)
		}
	}
}

func TestApply_ConsolidationRejectsUnknownAccountBeforeMerging(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]
	missing := int64(9999)

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{AccountID: &missing}); !errors.Is(err, storage.ErrValidation) {
		t.Fatalf("apply with an unknown account: err=%v, want ErrValidation", err)
	}
	for _, src := range []domain.Pool{a, b} {
		if got, found, _ := st.GetPool(ctx, src.ID); !found || got.CIDR != src.CIDR {
			t.Errorf("source pool %d is %+v after a rejected apply, want %s unchanged", src.ID, got, src.CIDR)
		}
	}
}

func TestApply_ConsolidationRetryFinishesPartialMerge(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	disc := storage.NewMemoryDiscoveryStore(st)
	failures := 1
	svc := NewRecommendationService(NewAnalysisService(st), storage.NewMemoryRecommendationStore(st), st,
		flakyLinks{disc, &failures}, storePlacer{st})
	acct, err := st.CreateAccount(ctx, domain.CreateAccount{Key: "aws:1", Name: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, &acct.ID)
	b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, &acct.ID)
	if err := disc.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		AccountID: acct.ID, Provider: "aws", ResourceType: domain.ResourceTypeSubnet, ResourceID: "subnet-b",
		CIDR: "10.0.1.128/26", PoolID: &b.ID, Status: domain.DiscoveryStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{Name: "merged"}); err == nil {
		t.Fatal("apply succeeded although moving the linked resource failed")
	}
	stored, _ := svc.store.GetRecommendation(ctx, rec.ID)
	if stored.Status != domain.RecommendationStatusPending {
		t.Fatalf("rec status %s after a failed resource move, want pending", stored.Status)
	}

	applied, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{Name: "merged"})
	if err != nil {
		t.Fatalf("retry after the merge committed: %v", err)
	}
	if applied.AppliedPoolID == nil || *applied.AppliedPoolID != a.ID {
		t.Errorf("retry applied pool %v, want the kept pool %d", applied.AppliedPoolID, a.ID)
	}
	kept, _, _ := st.GetPool(ctx, a.ID)
	if kept.CIDR != "10.0.1.0/24" || kept.Name != "merged" {
		t.Errorf("kept pool %+v, want 10.0.1.0/24 named merged", kept)
	}
	res, _, err := disc.ListDiscoveredResources(ctx, acct.ID, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].PoolID == nil || *res[0].PoolID != a.ID {
		t.Errorf("resource links to %v after the retry, want %d", res[0].PoolID, a.ID)
	}
}
