package planning

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestCheckCompliance_Overlap(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Parent",
		CIDR: "10.0.0.0/16",
		Type: domain.PoolTypeSupernet,
	})

	parentID := parent.ID
	c1, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Child A", CIDR: "10.0.0.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})
	c2, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Child B", CIDR: "10.0.0.128/25", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	report, err := svc.CheckCompliance(ctx, []int64{c1.ID, c2.ID}, false)
	if err != nil {
		t.Fatal(err)
	}

	hasOverlap := false
	for _, v := range report.Violations {
		if v.RuleID == "OVERLAP-001" {
			hasOverlap = true
		}
	}
	if !hasOverlap {
		t.Error("expected OVERLAP-001 violation for overlapping siblings")
	}
	if report.Failed == 0 {
		t.Error("expected at least one failure")
	}
}

func TestCheckCompliance_RFC1918(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	pool, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Public Pool",
		CIDR: "8.8.8.0/24",
		Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	report, err := svc.CheckCompliance(ctx, []int64{pool.ID}, false)
	if err != nil {
		t.Fatal(err)
	}

	hasRFC1918 := false
	for _, v := range report.Violations {
		if v.RuleID == "RFC1918-001" {
			hasRFC1918 = true
		}
	}
	if !hasRFC1918 {
		t.Error("expected RFC1918-001 warning for public address space")
	}
}

func TestCheckCompliance_RFC1918_Private(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	pool, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Private Pool",
		CIDR: "10.0.0.0/8",
		Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	report, err := svc.CheckCompliance(ctx, []int64{pool.ID}, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range report.Violations {
		if v.RuleID == "RFC1918-001" {
			t.Error("should not flag RFC1918 private address space")
		}
	}
}

func TestCheckCompliance_EmptyParent(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	pool, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Empty VPC",
		CIDR: "10.0.0.0/16",
		Type: domain.PoolTypeVPC,
	})

	svc := NewAnalysisService(store)
	report, err := svc.CheckCompliance(ctx, []int64{pool.ID}, false)
	if err != nil {
		t.Fatal(err)
	}

	hasEmpty := false
	for _, v := range report.Violations {
		if v.RuleID == "EMPTY-001" {
			hasEmpty = true
		}
	}
	if !hasEmpty {
		t.Error("expected EMPTY-001 warning for VPC with no children")
	}
}

func TestCheckCompliance_Naming(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	pool, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Named Pool",
		CIDR: "10.0.0.0/24",
		Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	report, err := svc.CheckCompliance(ctx, []int64{pool.ID}, false)
	if err != nil {
		t.Fatal(err)
	}

	// Should have NAME-002 (no description) but not NAME-001 (has name).
	hasName001 := false
	hasName002 := false
	for _, v := range report.Violations {
		if v.RuleID == "NAME-001" {
			hasName001 = true
		}
		if v.RuleID == "NAME-002" {
			hasName002 = true
		}
	}
	if hasName001 {
		t.Error("should not flag NAME-001 when pool has a name")
	}
	if !hasName002 {
		t.Error("expected NAME-002 for missing description")
	}
}

func TestCheckCompliance_AllPools(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	store.CreatePool(ctx, domain.CreatePool{
		Name: "Pool 1", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSubnet,
	})
	store.CreatePool(ctx, domain.CreatePool{
		Name: "Pool 2", CIDR: "172.16.0.0/16", Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	// Empty pool IDs = all pools.
	report, err := svc.CheckCompliance(ctx, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	if report.TotalChecks == 0 {
		t.Error("expected checks to run on all pools")
	}
}

func TestCheckCompliance_IncludeChildren(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Parent", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet,
	})

	parentID := parent.ID
	store.CreatePool(ctx, domain.CreatePool{
		Name: "Child", CIDR: "10.0.0.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	report, err := svc.CheckCompliance(ctx, []int64{parent.ID}, true)
	if err != nil {
		t.Fatal(err)
	}

	// Should check both parent and child.
	// Parent has 5 checks (overlap + rfc1918 + empty + name + desc),
	// child has 5 checks too.
	if report.TotalChecks < 8 { // at least some checks on 2 pools
		t.Errorf("expected checks on parent + child, got %d total checks", report.TotalChecks)
	}
}
