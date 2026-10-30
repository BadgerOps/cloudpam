//go:build postgres

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

// reopenStore opens a second pool from the connection string, the closest
// in-process stand-in for a restart; NewFromPool would share the writer's pool.
func reopenStore(t *testing.T) *Store {
	t.Helper()
	reopened, err := New(testDB.connStr)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

func mustCreatePool(t *testing.T, store *Store, name, cidr string, parentID *int64) domain.Pool {
	t.Helper()
	pool, err := store.CreatePool(context.Background(), domain.CreatePool{Name: name, CIDR: cidr, ParentID: parentID})
	if err != nil {
		t.Fatalf("create pool %s %s: %v", name, cidr, err)
	}
	return pool
}

func mustCreateRecommendation(t *testing.T, store *Store, rec domain.Recommendation) {
	t.Helper()
	if err := store.CreateRecommendation(context.Background(), rec); err != nil {
		t.Fatalf("create recommendation %s: %v", rec.ID, err)
	}
}

func listRecommendationIDs(t *testing.T, store *Store, filters domain.RecommendationFilters) ([]string, int) {
	t.Helper()
	items, total, err := store.ListRecommendations(context.Background(), filters)
	if err != nil {
		t.Fatalf("list %+v: %v", filters, err)
	}
	if items == nil {
		t.Fatalf("list %+v returned a nil slice; want non-nil", filters)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids, total
}

func TestRecommendationStorePersistsEveryFieldAcrossRestart(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	parent := mustCreatePool(t, testDB.store, "parent", "10.0.0.0/24", nil)
	child := mustCreatePool(t, testDB.store, "child", "10.0.0.0/26", &parent.ID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	want := domain.Recommendation{
		ID:            uuid.New().String(),
		PoolID:        parent.ID,
		Type:          domain.RecommendationTypeCompliance,
		Status:        domain.RecommendationStatusPending,
		Priority:      domain.RecommendationPriorityHigh,
		Title:         "Overlapping child",
		Description:   "child overlaps a sibling",
		SuggestedCIDR: "10.0.0.64/26",
		RuleID:        "OVERLAP-001",
		Score:         90,
		Metadata:      map[string]string{"source": "test", "rule": "overlap"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	mustCreateRecommendation(t, testDB.store, want)
	if err := testDB.store.UpdateRecommendationStatus(ctx, want.ID, domain.RecommendationStatusApplied, "", &child.ID); err != nil {
		t.Fatalf("update status: %v", err)
	}

	reopened := reopenStore(t)
	got, err := reopened.GetRecommendation(ctx, want.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if got.ID != want.ID || got.PoolID != want.PoolID || got.Type != want.Type || got.Priority != want.Priority ||
		got.Title != want.Title || got.Description != want.Description || got.SuggestedCIDR != want.SuggestedCIDR ||
		got.RuleID != want.RuleID || got.Score != want.Score || got.DismissReason != "" {
		t.Fatalf("fields changed across restart:\n got %+v\nwant %+v", *got, want)
	}
	if !reflect.DeepEqual(got.Metadata, want.Metadata) {
		t.Fatalf("metadata = %v, want %v", got.Metadata, want.Metadata)
	}
	if got.Status != domain.RecommendationStatusApplied {
		t.Fatalf("status = %s, want applied", got.Status)
	}
	if got.AppliedPoolID == nil || *got.AppliedPoolID != child.ID {
		t.Fatalf("applied pool id = %v, want %d", got.AppliedPoolID, child.ID)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %v, want %v", got.CreatedAt, now)
	}
	if !got.UpdatedAt.After(want.UpdatedAt) {
		t.Fatalf("updated_at %v did not move past %v on update", got.UpdatedAt, want.UpdatedAt)
	}

	ids, total := listRecommendationIDs(t, reopened, domain.RecommendationFilters{PoolID: parent.ID})
	if total != 1 || len(ids) != 1 || ids[0] != want.ID {
		t.Fatalf("list by pool = %v (total %d), want [%s] total 1", ids, total, want.ID)
	}

	if _, err := reopened.GetRecommendation(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("get missing (non-uuid id) err = %v, want ErrNotFound", err)
	}
	if err := reopened.UpdateRecommendationStatus(ctx, "missing", domain.RecommendationStatusDismissed, "x", nil); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("update missing err = %v, want ErrNotFound", err)
	}
}

type lockedPlacer struct{ s *Store }

func (p lockedPlacer) PlacePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	var out domain.Pool
	err := p.s.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
		var err error
		out, err = ps.CreatePool(ctx, in)
		return err
	})
	return out, err
}

func TestRecommendationServiceRoundTripsThroughPostgres(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	parent, err := s.CreatePool(ctx, domain.CreatePool{Name: "supernet", CIDR: "10.0.0.0/24", Description: "round trip"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	mustCreatePool(t, s, "child", "10.0.0.0/26", &parent.ID)

	svc := planning.NewRecommendationService(planning.NewAnalysisService(s), s, s, lockedPlacer{s})
	first, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{parent.ID}})
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	var allocations []domain.Recommendation
	for _, rec := range first.Items {
		if rec.Type == domain.RecommendationTypeAllocation {
			allocations = append(allocations, rec)
		}
	}
	if len(allocations) < 2 {
		t.Fatalf("first generate produced %d allocation recommendations, want at least 2 (free: 10.0.0.64/26, 10.0.0.128/25): %+v", len(allocations), first.Items)
	}

	applied, err := svc.Apply(ctx, allocations[0].ID, domain.ApplyRecommendationRequest{Name: "from-rec"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	dismissed, err := svc.Dismiss(ctx, allocations[1].ID, "not needed")
	if err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	second, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{parent.ID}})
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}

	reopened := reopenStore(t)

	gotApplied, err := reopened.GetRecommendation(ctx, applied.ID)
	if err != nil {
		t.Fatalf("get applied after restart: %v", err)
	}
	if gotApplied.Status != domain.RecommendationStatusApplied || gotApplied.AppliedPoolID == nil {
		t.Fatalf("applied rec after restart = %+v, want status applied with an applied pool", *gotApplied)
	}
	createdPool, found, err := reopened.GetPool(ctx, *gotApplied.AppliedPoolID)
	if err != nil || !found {
		t.Fatalf("applied pool %d: found=%v err=%v", *gotApplied.AppliedPoolID, found, err)
	}
	if createdPool.CIDR != gotApplied.SuggestedCIDR || createdPool.ParentID == nil || *createdPool.ParentID != parent.ID {
		t.Fatalf("applied pool = %+v, want cidr %s under parent %d", createdPool, gotApplied.SuggestedCIDR, parent.ID)
	}

	gotDismissed, err := reopened.GetRecommendation(ctx, dismissed.ID)
	if err != nil {
		t.Fatalf("get dismissed after restart: %v", err)
	}
	if gotDismissed.Status != domain.RecommendationStatusDismissed || gotDismissed.DismissReason != "not needed" {
		t.Fatalf("dismissed rec after restart = %+v, want status dismissed with reason", *gotDismissed)
	}

	for _, rec := range first.Items {
		if rec.ID == applied.ID || rec.ID == dismissed.ID {
			continue
		}
		if _, err := reopened.GetRecommendation(ctx, rec.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("stale pending %s survived regeneration: err = %v", rec.ID, err)
		}
	}

	wantPending := map[string]bool{}
	for _, rec := range second.Items {
		wantPending[rec.ID] = true
	}
	pendingIDs, pendingTotal := listRecommendationIDs(t, reopened, domain.RecommendationFilters{PoolID: parent.ID, Status: string(domain.RecommendationStatusPending)})
	if pendingTotal != second.Total || len(pendingIDs) != len(wantPending) {
		t.Fatalf("pending after restart = %v (total %d), want the second generate's %d items", pendingIDs, pendingTotal, second.Total)
	}
	for _, id := range pendingIDs {
		if !wantPending[id] {
			t.Fatalf("pending id %s is not from the second generate", id)
		}
	}
	if _, total := listRecommendationIDs(t, reopened, domain.RecommendationFilters{PoolID: parent.ID}); total != second.Total+2 {
		t.Fatalf("total for pool after restart = %d, want %d (second generate plus applied and dismissed)", total, second.Total+2)
	}
}

func TestRecommendationStoreListFiltersPagesAndScopesDeletes(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	p1 := mustCreatePool(t, s, "p1", "10.1.0.0/16", nil)
	p2 := mustCreatePool(t, s, "p2", "10.2.0.0/16", nil)
	p3 := mustCreatePool(t, s, "p3", "10.3.0.0/16", nil)

	t0 := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	seed := func(id string, poolID int64, typ domain.RecommendationType, status domain.RecommendationStatus, priority domain.RecommendationPriority, at time.Time) domain.Recommendation {
		rec := domain.Recommendation{
			ID: id, PoolID: poolID, Type: typ, Status: status, Priority: priority,
			Title: "rec " + id, CreatedAt: at, UpdatedAt: at,
		}
		mustCreateRecommendation(t, s, rec)
		return rec
	}
	a := seed("a", p1.ID, domain.RecommendationTypeAllocation, domain.RecommendationStatusPending, domain.RecommendationPriorityHigh, t0)
	seed("b", p1.ID, domain.RecommendationTypeCompliance, domain.RecommendationStatusApplied, domain.RecommendationPriorityLow, t0.Add(1*time.Minute))
	seed("c", p2.ID, domain.RecommendationTypeAllocation, domain.RecommendationStatusDismissed, domain.RecommendationPriorityMedium, t0.Add(2*time.Minute))
	seed("d", p2.ID, domain.RecommendationTypeAllocation, domain.RecommendationStatusPending, domain.RecommendationPriorityLow, t0.Add(3*time.Minute))
	batch := t0.Add(4 * time.Minute)
	for _, id := range []string{"x", "y", "z"} {
		seed(id, p3.ID, domain.RecommendationTypeAllocation, domain.RecommendationStatusPending, domain.RecommendationPriorityMedium, batch)
	}

	cases := []struct {
		name      string
		filters   domain.RecommendationFilters
		wantIDs   []string
		wantTotal int
	}{
		{"by pool", domain.RecommendationFilters{PoolID: p1.ID}, []string{"b", "a"}, 2},
		{"combined", domain.RecommendationFilters{PoolID: p2.ID, Type: "allocation", Status: "pending"}, []string{"d"}, 1},
		{"by type", domain.RecommendationFilters{Type: "compliance"}, []string{"b"}, 1},
		{"by priority", domain.RecommendationFilters{Priority: "low"}, []string{"d", "b"}, 2},
		{"by status", domain.RecommendationFilters{Status: "dismissed"}, []string{"c"}, 1},
		{"unknown pool", domain.RecommendationFilters{PoolID: 9999}, []string{}, 0},
		{"past the last page", domain.RecommendationFilters{Page: 9, PageSize: 3}, []string{}, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, total := listRecommendationIDs(t, s, tc.filters)
			if total != tc.wantTotal || !reflect.DeepEqual(ids, tc.wantIDs) {
				t.Fatalf("list = %v (total %d), want %v (total %d)", ids, total, tc.wantIDs, tc.wantTotal)
			}
		})
	}

	t.Run("defaults to page 1 of 50 newest first", func(t *testing.T) {
		items, total, err := s.ListRecommendations(ctx, domain.RecommendationFilters{Page: 0, PageSize: 0})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 7 || len(items) != 7 {
			t.Fatalf("got %d items total %d, want 7 and 7", len(items), total)
		}
		for i := 1; i < len(items); i++ {
			if items[i].CreatedAt.After(items[i-1].CreatedAt) {
				t.Fatalf("items not newest first at %d: %v after %v", i, items[i].CreatedAt, items[i-1].CreatedAt)
			}
		}
		if items[len(items)-1].ID != "a" {
			t.Fatalf("oldest item = %s, want a", items[len(items)-1].ID)
		}
	})

	t.Run("total ignores paging", func(t *testing.T) {
		ids, total := listRecommendationIDs(t, s, domain.RecommendationFilters{Page: 2, PageSize: 3})
		if total != 7 || len(ids) != 3 {
			t.Fatalf("page 2 of 3 = %v (total %d), want 3 items total 7", ids, total)
		}
	})

	t.Run("paging through tied timestamps visits each row once", func(t *testing.T) {
		seen := map[string]int{}
		for page := 1; page <= 3; page++ {
			ids, total := listRecommendationIDs(t, s, domain.RecommendationFilters{PoolID: p3.ID, Page: page, PageSize: 1})
			if total != 3 || len(ids) != 1 {
				t.Fatalf("page %d = %v (total %d), want one item total 3", page, ids, total)
			}
			seen[ids[0]]++
		}
		if !reflect.DeepEqual(seen, map[string]int{"x": 1, "y": 1, "z": 1}) {
			t.Fatalf("pages visited %v, want x, y, z once each", seen)
		}
	})

	t.Run("duplicate id conflicts", func(t *testing.T) {
		if err := s.CreateRecommendation(ctx, a); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("duplicate create err = %v, want ErrConflict", err)
		}
	})

	t.Run("delete pending is scoped to the pool and idempotent", func(t *testing.T) {
		if err := s.DeletePendingForPool(ctx, p1.ID); err != nil {
			t.Fatalf("delete pending: %v", err)
		}
		if ids, _ := listRecommendationIDs(t, s, domain.RecommendationFilters{PoolID: p1.ID}); !reflect.DeepEqual(ids, []string{"b"}) {
			t.Fatalf("p1 after delete = %v, want [b] (applied survives)", ids)
		}
		if ids, _ := listRecommendationIDs(t, s, domain.RecommendationFilters{PoolID: p2.ID}); !reflect.DeepEqual(ids, []string{"d", "c"}) {
			t.Fatalf("p2 after deleting p1's pending = %v, want [d c]", ids)
		}
		if err := s.DeletePendingForPool(ctx, p1.ID); err != nil {
			t.Fatalf("second delete pending err = %v, want nil", err)
		}
	})
}

func TestRecommendationStoreScopesByOrganization(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	storeA := testDB.store
	orgB := "00000000-0000-0000-0000-000000000222"
	if _, err := testDB.pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, plan)
		VALUES ($1, 'Other Org', 'other-org', 'free')
		ON CONFLICT (id) DO NOTHING`, orgB); err != nil {
		t.Fatalf("create second org: %v", err)
	}
	storeB := &Store{pool: testDB.pool, orgID: orgB}

	poolA := mustCreatePool(t, storeA, "a", "10.0.0.0/16", nil)
	poolB := mustCreatePool(t, storeB, "b", "10.0.0.0/16", nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	recA := domain.Recommendation{ID: "rec-a", PoolID: poolA.ID, Type: domain.RecommendationTypeAllocation, Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh, Title: "A", CreatedAt: now, UpdatedAt: now}
	recB := domain.Recommendation{ID: "rec-b", PoolID: poolB.ID, Type: domain.RecommendationTypeAllocation, Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh, Title: "B", CreatedAt: now, UpdatedAt: now}
	mustCreateRecommendation(t, storeA, recA)
	mustCreateRecommendation(t, storeB, recB)

	if ids, total := listRecommendationIDs(t, storeA, domain.RecommendationFilters{}); total != 1 || !reflect.DeepEqual(ids, []string{"rec-a"}) {
		t.Fatalf("org A list = %v (total %d), want [rec-a] total 1", ids, total)
	}
	if ids, total := listRecommendationIDs(t, storeB, domain.RecommendationFilters{}); total != 1 || !reflect.DeepEqual(ids, []string{"rec-b"}) {
		t.Fatalf("org B list = %v (total %d), want [rec-b] total 1", ids, total)
	}
	if _, err := storeA.GetRecommendation(ctx, recB.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("org A get of org B's rec err = %v, want ErrNotFound", err)
	}
	if err := storeA.UpdateRecommendationStatus(ctx, recB.ID, domain.RecommendationStatusDismissed, "cross-org", nil); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("org A update of org B's rec err = %v, want ErrNotFound", err)
	}
	if err := storeA.DeletePendingForPool(ctx, poolB.ID); err != nil {
		t.Fatalf("org A delete pending for org B's pool: %v", err)
	}
	got, err := storeB.GetRecommendation(ctx, recB.ID)
	if err != nil {
		t.Fatalf("org B's rec after org A's update and delete: %v", err)
	}
	if got.Status != domain.RecommendationStatusPending || got.DismissReason != "" {
		t.Fatalf("org B's rec was changed by org A: %+v", *got)
	}
}
