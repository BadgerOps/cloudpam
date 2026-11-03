package planning

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func setupTypedRecService(t *testing.T) (*RecommendationService, *storage.MemoryStore, *storage.MemoryDiscoveryStore) {
	t.Helper()
	st := storage.NewMemoryStore()
	recStore := storage.NewMemoryRecommendationStore(st)
	disc := storage.NewMemoryDiscoveryStore(st)
	svc := NewRecommendationService(NewAnalysisService(st), recStore, st, disc, storePlacer{st})
	return svc, st, disc
}

func mustPool(t *testing.T, st storage.Store, name, cidr string, parentID *int64, accountID *int64) domain.Pool {
	t.Helper()
	p, err := st.CreatePool(context.Background(), domain.CreatePool{Name: name, CIDR: cidr, ParentID: parentID, AccountID: accountID})
	if err != nil {
		t.Fatalf("create pool %s: %v", cidr, err)
	}
	return p
}

func generateFor(t *testing.T, svc *RecommendationService, poolIDs ...int64) []domain.Recommendation {
	t.Helper()
	resp, err := svc.Generate(context.Background(), domain.GenerateRecommendationsRequest{PoolIDs: poolIDs})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return resp.Items
}

func recsOfType(recs []domain.Recommendation, typ domain.RecommendationType) []domain.Recommendation {
	var out []domain.Recommendation
	for _, r := range recs {
		if r.Type == typ {
			out = append(out, r)
		}
	}
	return out
}

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergeCandidates(t *testing.T) {
	parent := mustPrefix(t, "10.0.0.0/16")
	acct1, acct2 := int64(1), int64(2)
	leaf := func(int64) bool { return true }
	pools := func(cidrs ...string) []domain.Pool {
		out := make([]domain.Pool, 0, len(cidrs))
		for i, c := range cidrs {
			out = append(out, domain.Pool{ID: int64(i + 1), Name: c, CIDR: c})
		}
		return out
	}

	t.Run("buddy halves merge into their aligned block", func(t *testing.T) {
		got := mergeCandidates(parent, pools("10.0.1.0/25", "10.0.1.128/25", "10.0.2.0/24"), leaf)
		if len(got) != 1 || got[0].merged.String() != "10.0.1.0/24" {
			t.Fatalf("got %+v, want one merge into 10.0.1.0/24", got)
		}
		if got[0].first.CIDR != "10.0.1.0/25" || got[0].second.CIDR != "10.0.1.128/25" {
			t.Errorf("sources %s and %s, want the two /25 halves", got[0].first.CIDR, got[0].second.CIDR)
		}
	})

	t.Run("adjacent blocks that straddle an alignment boundary do not merge", func(t *testing.T) {
		if got := mergeCandidates(parent, pools("10.0.1.128/25", "10.0.2.0/25"), leaf); len(got) != 0 {
			t.Errorf("got %+v, want none", got)
		}
	})

	t.Run("different prefix lengths do not merge", func(t *testing.T) {
		if got := mergeCandidates(parent, pools("10.0.1.0/25", "10.0.1.128/26"), leaf); len(got) != 0 {
			t.Errorf("got %+v, want none", got)
		}
	})

	t.Run("halves of the parent itself do not merge", func(t *testing.T) {
		if got := mergeCandidates(parent, pools("10.0.0.0/17", "10.0.128.0/17"), leaf); len(got) != 0 {
			t.Errorf("got %+v, want none: the merged block would equal the parent", got)
		}
	})

	t.Run("a source with children does not merge", func(t *testing.T) {
		notLeaf := func(id int64) bool { return id != 1 }
		if got := mergeCandidates(parent, pools("10.0.1.0/25", "10.0.1.128/25"), notLeaf); len(got) != 0 {
			t.Errorf("got %+v, want none", got)
		}
	})

	t.Run("sources assigned to different accounts do not merge", func(t *testing.T) {
		ps := pools("10.0.1.0/25", "10.0.1.128/25")
		ps[0].AccountID, ps[1].AccountID = &acct1, &acct2
		if got := mergeCandidates(parent, ps, leaf); len(got) != 0 {
			t.Errorf("got %+v, want none", got)
		}
		ps[1].AccountID = &acct1
		if got := mergeCandidates(parent, ps, leaf); len(got) != 1 {
			t.Errorf("same account: got %+v, want one", got)
		}
	})
}

func TestShrinkTarget(t *testing.T) {
	cases := []struct {
		name     string
		pool     string
		children []string
		want     string
		ok       bool
	}{
		{"two low /24s in a /16 fit a /23", "10.0.0.0/16", []string{"10.0.0.0/24", "10.0.1.0/24"}, "10.0.0.0/23", true},
		{"children spread across the whole pool", "10.0.0.0/16", []string{"10.0.0.0/24", "10.0.255.0/24"}, "", false},
		{"a single child widens by one bit so the pool stays larger than it", "10.0.0.0/16", []string{"10.0.4.0/24"}, "10.0.4.0/23", true},
		{"children filling the lower half still fit a /17", "10.0.0.0/16", []string{"10.0.0.0/18", "10.0.64.0/18"}, "10.0.0.0/17", true},
		{"no children", "10.0.0.0/16", nil, "", false},
		{"halves of the pool cannot shrink", "10.0.0.0/16", []string{"10.0.0.0/17"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			children := make([]netip.Prefix, 0, len(tc.children))
			for _, c := range tc.children {
				children = append(children, mustPrefix(t, c))
			}
			got, ok := shrinkTarget(mustPrefix(t, tc.pool), children)
			if ok != tc.ok {
				t.Fatalf("ok=%v, want %v (got %s)", ok, tc.ok, got)
			}
			if ok && got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGenerate_ConsolidationRecs(t *testing.T) {
	svc, st, _ := setupTypedRecService(t)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	mustPool(t, st, "c", "10.0.2.0/24", &parent.ID, nil)

	recs := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)
	if len(recs) != 1 {
		t.Fatalf("got %d consolidation recs, want 1: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.SuggestedCIDR != "10.0.1.0/24" {
		t.Errorf("suggested_cidr %q, want 10.0.1.0/24", rec.SuggestedCIDR)
	}
	if rec.PoolID != parent.ID || rec.Status != domain.RecommendationStatusPending {
		t.Errorf("rec on pool %d with status %s, want parent %d pending", rec.PoolID, rec.Status, parent.ID)
	}
	if rec.Score < 1 || rec.Score > 100 || rec.Priority == "" {
		t.Errorf("score %d priority %q", rec.Score, rec.Priority)
	}
	if want := a.CIDR + "," + b.CIDR; rec.Metadata["source_cidrs"] != want {
		t.Errorf("source_cidrs %q, want %q", rec.Metadata["source_cidrs"], want)
	}

	stored, err := svc.store.GetRecommendation(context.Background(), rec.ID)
	if err != nil || stored.Type != domain.RecommendationTypeConsolidation {
		t.Fatalf("stored rec: %+v err=%v", stored, err)
	}
}

func TestGenerate_ResizeRecs(t *testing.T) {
	t.Run("a pool far below its size shrinks to the block its children fill", func(t *testing.T) {
		svc, st, _ := setupTypedRecService(t)
		pool := mustPool(t, st, "Sparse", "10.0.0.0/16", nil, nil)
		mustPool(t, st, "a", "10.0.0.0/24", &pool.ID, nil)
		mustPool(t, st, "b", "10.0.1.0/24", &pool.ID, nil)

		recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeResize)
		if len(recs) != 1 {
			t.Fatalf("got %d resize recs, want 1", len(recs))
		}
		if recs[0].SuggestedCIDR != "10.0.0.0/23" {
			t.Errorf("suggested_cidr %q, want 10.0.0.0/23", recs[0].SuggestedCIDR)
		}
		if recs[0].Metadata["current_cidr"] != "10.0.0.0/16" {
			t.Errorf("current_cidr %q, want 10.0.0.0/16", recs[0].Metadata["current_cidr"])
		}
		if recs[0].Priority != domain.RecommendationPriorityHigh {
			t.Errorf("priority %s for a 0.8%% utilized /16, want high", recs[0].Priority)
		}
	})

	t.Run("a well used pool is left alone", func(t *testing.T) {
		svc, st, _ := setupTypedRecService(t)
		pool := mustPool(t, st, "Busy", "10.0.0.0/24", nil, nil)
		mustPool(t, st, "a", "10.0.0.0/26", &pool.ID, nil)
		mustPool(t, st, "b", "10.0.0.64/27", &pool.ID, nil)

		if recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeResize); len(recs) != 0 {
			t.Errorf("got %+v, want no resize rec at 37.5%% utilization", recs)
		}
	})

	t.Run("a leaf pool is not resized", func(t *testing.T) {
		svc, st, _ := setupTypedRecService(t)
		pool := mustPool(t, st, "Leaf", "10.0.0.0/16", nil, nil)
		if recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeResize); len(recs) != 0 {
			t.Errorf("got %+v, want none", recs)
		}
	})
}

func TestGenerate_ReclaimRecs(t *testing.T) {
	idle := func(svc *RecommendationService, d time.Duration) {
		base := time.Now()
		svc.now = func() time.Time { return base.Add(d) }
	}

	t.Run("an idle empty unlinked pool is reclaimable", func(t *testing.T) {
		svc, st, _ := setupTypedRecService(t)
		pool := mustPool(t, st, "Idle", "10.0.5.0/24", nil, nil)
		idle(svc, 45*24*time.Hour)

		recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim)
		if len(recs) != 1 {
			t.Fatalf("got %d reclaim recs, want 1", len(recs))
		}
		if recs[0].SuggestedCIDR != "10.0.5.0/24" || recs[0].Metadata["idle_days"] != "45" {
			t.Errorf("rec %+v, want 10.0.5.0/24 idle 45 days", recs[0])
		}
	})

	t.Run("a recently changed pool is not reclaimable", func(t *testing.T) {
		svc, st, _ := setupTypedRecService(t)
		pool := mustPool(t, st, "Fresh", "10.0.5.0/24", nil, nil)
		if recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim); len(recs) != 0 {
			t.Errorf("got %+v, want none", recs)
		}
	})

	t.Run("a pool with children is not reclaimable", func(t *testing.T) {
		svc, st, _ := setupTypedRecService(t)
		pool := mustPool(t, st, "Parent", "10.0.0.0/16", nil, nil)
		mustPool(t, st, "child", "10.0.0.0/24", &pool.ID, nil)
		idle(svc, 45*24*time.Hour)
		if recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim); len(recs) != 0 {
			t.Errorf("got %+v, want none", recs)
		}
	})

	t.Run("a pool with a linked discovered resource is not reclaimable", func(t *testing.T) {
		svc, st, disc := setupTypedRecService(t)
		acct, err := st.CreateAccount(context.Background(), domain.CreateAccount{Key: "aws:1", Name: "prod"})
		if err != nil {
			t.Fatal(err)
		}
		pool := mustPool(t, st, "Linked", "10.0.5.0/24", nil, &acct.ID)
		if err := disc.UpsertDiscoveredResource(context.Background(), domain.DiscoveredResource{
			AccountID: acct.ID, Provider: "aws", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-1",
			CIDR: "10.0.5.0/24", PoolID: &pool.ID, Status: domain.DiscoveryStatusActive,
		}); err != nil {
			t.Fatal(err)
		}
		idle(svc, 45*24*time.Hour)
		if recs := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim); len(recs) != 0 {
			t.Errorf("got %+v, want none", recs)
		}
	})
}

func TestApply_Consolidation(t *testing.T) {
	ctx := context.Background()
	svc, st, disc := setupTypedRecService(t)
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

	applied, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Status != domain.RecommendationStatusApplied || applied.AppliedPoolID == nil || *applied.AppliedPoolID != a.ID {
		t.Fatalf("applied rec %+v, want applied to the kept pool %d", applied, a.ID)
	}
	merged, found, _ := st.GetPool(ctx, a.ID)
	if !found || merged.CIDR != "10.0.1.0/24" || merged.ParentID == nil || *merged.ParentID != parent.ID {
		t.Errorf("kept pool %+v, want 10.0.1.0/24 under %d", merged, parent.ID)
	}
	if merged.Name != "a" || merged.AccountID == nil || *merged.AccountID != acct.ID {
		t.Errorf("kept pool name %q account %v, want its own name and account", merged.Name, merged.AccountID)
	}
	if _, found, _ := st.GetPool(ctx, b.ID); found {
		t.Errorf("absorbed pool %d still exists after the merge", b.ID)
	}
	res, _, err := disc.ListDiscoveredResources(ctx, acct.ID, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].PoolID == nil || *res[0].PoolID != a.ID {
		t.Errorf("resource linked to the absorbed pool now links to %v, want %d", res[0].PoolID, a.ID)
	}
}

func TestApply_ConsolidationRenamesKeptPool(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{Name: "merged"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	kept, _, _ := st.GetPool(ctx, a.ID)
	if kept.Name != "merged" || kept.AccountID != nil {
		t.Errorf("kept pool name %q account %v, want merged and no account", kept.Name, kept.AccountID)
	}
}

func TestApply_ConsolidationRefusesUsedMergedCIDR(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	b := mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]
	mustPool(t, st, "elsewhere", "10.0.1.0/24", nil, nil)

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("apply onto a used CIDR: err=%v, want ErrConflict", err)
	}
	for _, src := range []domain.Pool{a, b} {
		if got, found, _ := st.GetPool(ctx, src.ID); !found || got.CIDR != src.CIDR {
			t.Errorf("source pool %d is %+v after a refused apply, want %s unchanged", src.ID, got, src.CIDR)
		}
	}
}

func TestApply_ConsolidationRefusesChangedSources(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	parent := mustPool(t, st, "Corp", "10.0.0.0/16", nil, nil)
	a := mustPool(t, st, "a", "10.0.1.0/25", &parent.ID, nil)
	mustPool(t, st, "b", "10.0.1.128/25", &parent.ID, nil)
	rec := recsOfType(generateFor(t, svc, parent.ID), domain.RecommendationTypeConsolidation)[0]

	mustPool(t, st, "grandchild", "10.0.1.0/26", &a.ID, nil)

	_, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("apply after a source grew children: err=%v, want ErrConflict", err)
	}
	if _, found, _ := st.GetPool(ctx, a.ID); !found {
		t.Error("source pool was deleted although the apply was refused")
	}
	stored, _ := svc.store.GetRecommendation(ctx, rec.ID)
	if stored.Status != domain.RecommendationStatusPending {
		t.Errorf("rec status %s, want pending", stored.Status)
	}
}

func TestApply_Resize(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	pool := mustPool(t, st, "Sparse", "10.0.0.0/16", nil, nil)
	mustPool(t, st, "a", "10.0.0.0/24", &pool.ID, nil)
	mustPool(t, st, "b", "10.0.1.0/24", &pool.ID, nil)
	rec := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeResize)[0]

	applied, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.AppliedPoolID == nil || *applied.AppliedPoolID != pool.ID {
		t.Errorf("applied pool id %v, want the resized pool %d", applied.AppliedPoolID, pool.ID)
	}
	got, _, _ := st.GetPool(ctx, pool.ID)
	if got.CIDR != "10.0.0.0/23" {
		t.Errorf("pool cidr %q after resize, want 10.0.0.0/23", got.CIDR)
	}
	children, _ := st.GetPoolChildren(ctx, pool.ID)
	if len(children) != 2 {
		t.Errorf("pool kept %d children, want 2", len(children))
	}
}

func TestApply_ResizeRefusesWhenChildrenMoved(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	pool := mustPool(t, st, "Sparse", "10.0.0.0/16", nil, nil)
	mustPool(t, st, "a", "10.0.0.0/24", &pool.ID, nil)
	rec := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeResize)[0]

	mustPool(t, st, "far", "10.0.200.0/24", &pool.ID, nil)

	_, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("err=%v, want ErrConflict", err)
	}
	got, _, _ := st.GetPool(ctx, pool.ID)
	if got.CIDR != "10.0.0.0/16" {
		t.Errorf("pool cidr changed to %q although the apply was refused", got.CIDR)
	}
}

func TestApply_Reclaim(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	pool := mustPool(t, st, "Idle", "10.0.5.0/24", nil, nil)
	base := time.Now()
	svc.now = func() time.Time { return base.Add(200 * 24 * time.Hour) }
	rec := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim)[0]
	if rec.Priority != domain.RecommendationPriorityHigh {
		t.Errorf("priority %s for a /24 idle 200 days, want high", rec.Priority)
	}

	applied, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Status != domain.RecommendationStatusApplied || applied.AppliedPoolID != nil {
		t.Errorf("applied rec %+v, want applied without a pool id", applied)
	}
	if _, found, _ := st.GetPool(ctx, pool.ID); found {
		t.Error("pool still exists after reclaim")
	}
}

func TestApply_ReclaimRefusesNewChildren(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	pool := mustPool(t, st, "Idle", "10.0.0.0/16", nil, nil)
	base := time.Now()
	svc.now = func() time.Time { return base.Add(45 * 24 * time.Hour) }
	rec := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim)[0]

	mustPool(t, st, "child", "10.0.0.0/24", &pool.ID, nil)

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("err=%v, want ErrConflict", err)
	}
	if _, found, _ := st.GetPool(ctx, pool.ID); !found {
		t.Error("pool was deleted although the apply was refused")
	}
}

func TestDismiss_NewTypes(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupTypedRecService(t)
	pool := mustPool(t, st, "Idle", "10.0.5.0/24", nil, nil)
	base := time.Now()
	svc.now = func() time.Time { return base.Add(45 * 24 * time.Hour) }
	rec := recsOfType(generateFor(t, svc, pool.ID), domain.RecommendationTypeReclaim)[0]

	dismissed, err := svc.Dismiss(ctx, rec.ID, "still needed")
	if err != nil || dismissed.Status != domain.RecommendationStatusDismissed {
		t.Fatalf("dismiss: %+v err=%v", dismissed, err)
	}
	if _, found, _ := st.GetPool(ctx, pool.ID); !found {
		t.Error("dismissing a reclaim deleted the pool")
	}
}
