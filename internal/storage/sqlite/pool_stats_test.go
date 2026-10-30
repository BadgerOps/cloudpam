//go:build sqlite

package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_fk=1&cache=shared"
	s, err := New(dsn)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func timeoutCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

type statsFixture struct {
	parent, child, grandchild domain.Pool
}

func createThreeLevelPools(t *testing.T, s *Store) statsFixture {
	t.Helper()
	ctx := context.Background()
	parent, err := s.CreatePool(ctx, domain.CreatePool{Name: "parent", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := s.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: "10.0.1.0/24", ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	grandchild, err := s.CreatePool(ctx, domain.CreatePool{Name: "grandchild", CIDR: "10.0.1.0/26", ParentID: &child.ID})
	if err != nil {
		t.Fatalf("create grandchild: %v", err)
	}
	return statsFixture{parent: parent, child: child, grandchild: grandchild}
}

var (
	parentStats = domain.PoolStats{
		TotalIPs:       65536,
		UsedIPs:        256,
		AvailableIPs:   65280,
		Utilization:    0.390625,
		ChildCount:     2,
		DirectChildren: 1,
	}
	childStats = domain.PoolStats{
		TotalIPs:       256,
		UsedIPs:        64,
		AvailableIPs:   192,
		Utilization:    25,
		ChildCount:     1,
		DirectChildren: 1,
	}
	grandchildStats = domain.PoolStats{
		TotalIPs:     64,
		AvailableIPs: 64,
	}
)

func TestGetPoolHierarchyWithChildrenReturnsStats(t *testing.T) {
	s := newTestStore(t)
	f := createThreeLevelPools(t, s)

	tree, err := s.GetPoolHierarchy(timeoutCtx(t), nil)
	if err != nil {
		t.Fatalf("GetPoolHierarchy: %v", err)
	}
	if len(tree) != 1 || tree[0].ID != f.parent.ID {
		t.Fatalf("expected one root %d, got %+v", f.parent.ID, tree)
	}
	root := tree[0]
	if root.Stats != parentStats {
		t.Errorf("parent stats = %+v, want %+v", root.Stats, parentStats)
	}
	if len(root.Children) != 1 || root.Children[0].ID != f.child.ID {
		t.Fatalf("expected child %d under parent, got %+v", f.child.ID, root.Children)
	}
	child := root.Children[0]
	if child.Stats != childStats {
		t.Errorf("child stats = %+v, want %+v", child.Stats, childStats)
	}
	if len(child.Children) != 1 || child.Children[0].ID != f.grandchild.ID {
		t.Fatalf("expected grandchild %d under child, got %+v", f.grandchild.ID, child.Children)
	}
	if got := child.Children[0].Stats; got != grandchildStats {
		t.Errorf("grandchild stats = %+v, want %+v", got, grandchildStats)
	}

	sub, err := s.GetPoolHierarchy(timeoutCtx(t), &f.child.ID)
	if err != nil {
		t.Fatalf("GetPoolHierarchy(child): %v", err)
	}
	if len(sub) != 1 || sub[0].ID != f.child.ID || sub[0].Stats != childStats {
		t.Errorf("subtree = %+v, want child %d with %+v", sub, f.child.ID, childStats)
	}
}

func TestGetPoolWithStatsCountsDescendants(t *testing.T) {
	s := newTestStore(t)
	f := createThreeLevelPools(t, s)

	got, err := s.GetPoolWithStats(timeoutCtx(t), f.parent.ID)
	if err != nil {
		t.Fatalf("GetPoolWithStats: %v", err)
	}
	if got.ID != f.parent.ID || got.Stats != parentStats {
		t.Errorf("got %+v, want pool %d with %+v", got, f.parent.ID, parentStats)
	}
}

func TestCalculatePoolUtilizationCountsDescendants(t *testing.T) {
	s := newTestStore(t)
	f := createThreeLevelPools(t, s)

	got, err := s.CalculatePoolUtilization(timeoutCtx(t), f.parent.ID)
	if err != nil {
		t.Fatalf("CalculatePoolUtilization: %v", err)
	}
	if *got != parentStats {
		t.Errorf("got %+v, want %+v", *got, parentStats)
	}
}

func TestUpdateAccountPersistsChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "acct-1", Name: "before", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	updated, ok, err := s.UpdateAccount(timeoutCtx(t), created.ID, domain.Account{
		Name:        "after",
		Provider:    "gcp",
		Description: "moved",
		Regions:     []string{"us-east1"},
	})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if !ok {
		t.Fatalf("UpdateAccount reported account %d missing", created.ID)
	}
	if updated.Name != "after" || updated.Provider != "gcp" {
		t.Errorf("returned account = %+v, want name after / provider gcp", updated)
	}

	stored, ok, err := s.GetAccount(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("GetAccount: ok=%v err=%v", ok, err)
	}
	want := domain.Account{
		ID:          created.ID,
		Key:         "acct-1",
		Name:        "after",
		Provider:    "gcp",
		Description: "moved",
		Regions:     []string{"us-east1"},
	}
	if updated.CreatedAt.IsZero() || !updated.CreatedAt.Equal(stored.CreatedAt) {
		t.Errorf("returned created_at = %v, want stored %v", updated.CreatedAt, stored.CreatedAt)
	}
	stored.CreatedAt, stored.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(stored, want) {
		t.Errorf("stored account = %+v, want %+v", stored, want)
	}
}
