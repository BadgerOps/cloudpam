package planning

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// newDeepHierarchy builds root -> level1 -> level2 -> level3 and returns the
// pools in depth order.
func newDeepHierarchy(t *testing.T, ctx context.Context, store storage.Store) []domain.Pool {
	t.Helper()

	root, err := store.CreatePool(ctx, domain.CreatePool{
		Name: "Root", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet,
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	pools := []domain.Pool{root}
	specs := []struct {
		name string
		cidr string
	}{
		{"Level 1", "10.0.0.0/20"},
		{"Level 2", "10.0.0.0/24"},
		{"Level 3", "10.0.0.0/26"},
	}
	for _, spec := range specs {
		parentID := pools[len(pools)-1].ID
		p, err := store.CreatePool(ctx, domain.CreatePool{
			Name: spec.name, CIDR: spec.cidr, ParentID: &parentID, Type: domain.PoolTypeSubnet,
		})
		if err != nil {
			t.Fatalf("create %s: %v", spec.name, err)
		}
		pools = append(pools, p)
	}
	return pools
}

func TestResolvePoolsIncludesDescendantsAtEveryLevel(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	pools := newDeepHierarchy(t, ctx, store)

	svc := NewAnalysisService(store)
	resolved, err := svc.resolvePools(ctx, []int64{pools[0].ID}, true)
	if err != nil {
		t.Fatalf("resolvePools: %v", err)
	}

	got := map[int64]bool{}
	for _, p := range resolved {
		got[p.ID] = true
	}
	for _, want := range pools {
		if !got[want.ID] {
			t.Errorf("pool %q (id %d) missing from resolved set", want.Name, want.ID)
		}
	}
	if len(resolved) != len(pools) {
		t.Errorf("len(resolved) = %d, want %d", len(resolved), len(pools))
	}
}

func TestResolvePoolsDeduplicatesOverlappingRequests(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	pools := newDeepHierarchy(t, ctx, store)

	svc := NewAnalysisService(store)
	// Ask for a pool and one of its own descendants at the same time.
	resolved, err := svc.resolvePools(ctx, []int64{pools[0].ID, pools[2].ID}, true)
	if err != nil {
		t.Fatalf("resolvePools: %v", err)
	}

	seen := map[int64]int{}
	for _, p := range resolved {
		seen[p.ID]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("pool %d appears %d times, want 1", id, count)
		}
	}
	if len(seen) != len(pools) {
		t.Errorf("distinct pools = %d, want %d", len(seen), len(pools))
	}
}

func TestResolvePoolsWithoutChildrenReturnsOnlyRequested(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	pools := newDeepHierarchy(t, ctx, store)

	svc := NewAnalysisService(store)
	resolved, err := svc.resolvePools(ctx, []int64{pools[0].ID}, false)
	if err != nil {
		t.Fatalf("resolvePools: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != pools[0].ID {
		t.Fatalf("resolved = %v, want only root pool %d", resolved, pools[0].ID)
	}
}

func TestCheckComplianceCoversGrandchildren(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	pools := newDeepHierarchy(t, ctx, store)

	svc := NewAnalysisService(store)
	shallow, err := svc.CheckCompliance(ctx, []int64{pools[0].ID}, false)
	if err != nil {
		t.Fatalf("CheckCompliance without children: %v", err)
	}
	deep, err := svc.CheckCompliance(ctx, []int64{pools[0].ID}, true)
	if err != nil {
		t.Fatalf("CheckCompliance with children: %v", err)
	}

	// Every pool contributes the same fixed number of checks, so the deep run
	// must scale with the full descendant count, not just the direct children.
	perPool := shallow.TotalChecks
	if perPool == 0 {
		t.Fatal("expected the shallow run to record checks")
	}
	if want := perPool * len(pools); deep.TotalChecks != want {
		t.Errorf("TotalChecks = %d, want %d (%d pools)", deep.TotalChecks, want, len(pools))
	}
}
