package planning

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestAnalyze_Empty(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	svc := NewAnalysisService(store)

	report, err := svc.Analyze(ctx, AnalysisRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if report.Summary.HealthScore != 100 {
		t.Errorf("health_score = %d, want 100 for empty store", report.Summary.HealthScore)
	}
	if report.Summary.TotalPools != 0 {
		t.Errorf("total_pools = %d, want 0", report.Summary.TotalPools)
	}
}

func TestAnalyze_RealisticHierarchy(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	// Create a realistic hierarchy: /16 → two /24 children
	root, _ := store.CreatePool(ctx, domain.CreatePool{
		Name:        "Corp Network",
		CIDR:        "10.0.0.0/16",
		Type:        domain.PoolTypeSupernet,
		Description: "main corporate network",
	})

	rootID := root.ID
	child1, _ := store.CreatePool(ctx, domain.CreatePool{
		Name:        "Production",
		CIDR:        "10.0.0.0/24",
		ParentID:    &rootID,
		Type:        domain.PoolTypeSubnet,
		Description: "production subnet",
	})

	child2, _ := store.CreatePool(ctx, domain.CreatePool{
		Name:        "Development",
		CIDR:        "10.0.1.0/24",
		ParentID:    &rootID,
		Type:        domain.PoolTypeSubnet,
		Description: "development subnet",
	})

	svc := NewAnalysisService(store)
	report, err := svc.Analyze(ctx, AnalysisRequest{
		PoolIDs:         []int64{root.ID},
		IncludeChildren: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Summary checks.
	if report.Summary.TotalPools != 3 { // root + 2 children
		t.Errorf("total_pools = %d, want 3", report.Summary.TotalPools)
	}
	if report.Summary.TotalAddresses != 65536 { // /16
		t.Errorf("total_addresses = %d, want 65536", report.Summary.TotalAddresses)
	}
	if report.Summary.UsedAddresses != 512 { // 2 * /24
		t.Errorf("used_addresses = %d, want 512", report.Summary.UsedAddresses)
	}
	if report.Summary.HealthScore > 100 || report.Summary.HealthScore < 0 {
		t.Errorf("health_score = %d, should be 0-100", report.Summary.HealthScore)
	}

	// Gap analysis.
	if len(report.GapAnalyses) != 1 {
		t.Fatalf("gap_analyses count = %d, want 1", len(report.GapAnalyses))
	}
	gap := report.GapAnalyses[0]
	if gap.PoolID != root.ID {
		t.Errorf("gap pool_id = %d, want %d", gap.PoolID, root.ID)
	}
	if len(gap.AllocatedBlocks) != 2 {
		t.Errorf("allocated_blocks = %d, want 2", len(gap.AllocatedBlocks))
	}

	// Fragmentation.
	if report.Fragmentation == nil {
		t.Error("expected fragmentation analysis")
	}

	// Compliance.
	if report.Compliance == nil {
		t.Error("expected compliance report")
	}

	_ = child1
	_ = child2
}

func TestAnalyze_SpecificPools(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	p1, err := store.CreatePool(ctx, domain.CreatePool{
		Name: "Pool A", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSubnet,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool(ctx, domain.CreatePool{
		Name: "Pool B", CIDR: "172.16.0.0/16", Type: domain.PoolTypeSubnet,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(store)
	report, err := svc.Analyze(ctx, AnalysisRequest{
		PoolIDs: []int64{p1.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Only Pool A should be analyzed.
	if report.Summary.TotalPools != 1 {
		t.Errorf("total_pools = %d, want 1", report.Summary.TotalPools)
	}
}

func TestAnalyze_NotFound(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	svc := NewAnalysisService(store)

	_, err := svc.Analyze(ctx, AnalysisRequest{PoolIDs: []int64{999}})
	if err == nil {
		t.Error("expected error for non-existent pool")
	}
}

func TestAnalyze_HealthScoreDeduction(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	// Create overlapping siblings to trigger compliance errors.
	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Parent", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet,
	})

	parentID := parent.ID
	if _, err := store.CreatePool(ctx, domain.CreatePool{
		Name: "Overlap A", CIDR: "10.0.0.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool(ctx, domain.CreatePool{
		Name: "Overlap B", CIDR: "10.0.0.128/25", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(store)
	report, err := svc.Analyze(ctx, AnalysisRequest{
		PoolIDs:         []int64{parent.ID},
		IncludeChildren: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Summary.HealthScore >= 100 {
		t.Errorf("health_score = %d, expected < 100 due to compliance violations", report.Summary.HealthScore)
	}
	if report.Summary.ErrorCount == 0 {
		t.Error("expected error count > 0 for overlapping siblings")
	}
}
