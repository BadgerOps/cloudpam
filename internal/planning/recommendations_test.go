package planning

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func setupRecService(t *testing.T) (*RecommendationService, *storage.MemoryStore) {
	t.Helper()
	st := storage.NewMemoryStore()
	recStore := storage.NewMemoryRecommendationStore(st)
	analysisSvc := NewAnalysisService(st)
	recSvc := NewRecommendationService(analysisSvc, recStore, st)
	return recSvc, st
}

func TestGenerate_AllocationRecs(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	parent, _ := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp Network", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "main network",
	})
	parentID := parent.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Prod", CIDR: "10.0.0.0/26", ParentID: &parentID, Type: domain.PoolTypeSubnet,
		Description: "prod",
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{parent.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should have allocation recs for available blocks.
	var allocCount int
	for _, r := range resp.Items {
		if r.Type == domain.RecommendationTypeAllocation {
			allocCount++
			if r.SuggestedCIDR == "" {
				t.Error("allocation rec missing suggested_cidr")
			}
			if r.Score < 0 || r.Score > 100 {
				t.Errorf("score %d out of range", r.Score)
			}
			if r.Status != domain.RecommendationStatusPending {
				t.Errorf("expected pending status, got %s", r.Status)
			}
		}
	}
	if allocCount == 0 {
		t.Error("expected at least one allocation recommendation")
	}
}

func TestGenerate_ComplianceRecs(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	// Create pool with public IP to trigger RFC1918 warning.
	pool, _ := st.CreatePool(ctx, domain.CreatePool{
		Name: "Public", CIDR: "8.8.8.0/24", Type: domain.PoolTypeSubnet,
	})

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{pool.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	var complianceCount int
	for _, r := range resp.Items {
		if r.Type == domain.RecommendationTypeCompliance {
			complianceCount++
			if r.RuleID == "" {
				t.Error("compliance rec missing rule_id")
			}
		}
	}
	if complianceCount == 0 {
		t.Error("expected at least one compliance recommendation for non-RFC1918 pool")
	}
}

func TestGenerate_IdempotentRegeneration(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, _ := st.CreatePool(ctx, domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	// Generate twice — second should clear pending from first.
	resp1, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{pool.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp2, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{pool.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp1.Total != resp2.Total {
		t.Errorf("regeneration should produce same count: first=%d, second=%d", resp1.Total, resp2.Total)
	}
}

func TestApply_AllocationCreatesPool(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	parent, _ := st.CreatePool(ctx, domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "net",
	})

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{parent.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Find an allocation rec.
	var allocRec *domain.Recommendation
	for _, r := range resp.Items {
		if r.Type == domain.RecommendationTypeAllocation {
			allocRec = &r
			break
		}
	}
	if allocRec == nil {
		t.Fatal("no allocation recommendation found")
	}

	applied, err := svc.Apply(ctx, allocRec.ID, domain.ApplyRecommendationRequest{
		Name: "New Subnet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied.Status != domain.RecommendationStatusApplied {
		t.Errorf("expected applied status, got %s", applied.Status)
	}
	if applied.AppliedPoolID == nil {
		t.Error("expected applied_pool_id to be set")
	}

	// Verify pool was actually created.
	newPool, found, err := st.GetPool(ctx, *applied.AppliedPoolID)
	if err != nil || !found {
		t.Fatal("applied pool not found in store")
	}
	if newPool.CIDR != allocRec.SuggestedCIDR {
		t.Errorf("pool CIDR = %s, want %s", newPool.CIDR, allocRec.SuggestedCIDR)
	}
}

func TestApply_CannotApplyTwice(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, _ := st.CreatePool(ctx, domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "net",
	})

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{pool.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	var rec *domain.Recommendation
	for _, r := range resp.Items {
		rec = &r
		break
	}
	if rec == nil {
		t.Skip("no recommendations generated")
	}

	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(ctx, rec.ID, domain.ApplyRecommendationRequest{}); err == nil {
		t.Error("expected error applying already-applied recommendation")
	}
}

func TestDismiss_WithReason(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, _ := st.CreatePool(ctx, domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet,
		Description: "net",
	})

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{pool.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	var rec *domain.Recommendation
	for _, r := range resp.Items {
		rec = &r
		break
	}
	if rec == nil {
		t.Skip("no recommendations generated")
	}

	dismissed, err := svc.Dismiss(ctx, rec.ID, "not applicable")
	if err != nil {
		t.Fatal(err)
	}
	if dismissed.Status != domain.RecommendationStatusDismissed {
		t.Errorf("expected dismissed, got %s", dismissed.Status)
	}
	if dismissed.DismissReason != "not applicable" {
		t.Errorf("expected reason 'not applicable', got %q", dismissed.DismissReason)
	}
}

func TestScoreAllocation_Alignment(t *testing.T) {
	svc, st := setupRecService(t)

	pool, _ := st.CreatePool(context.Background(), domain.CreatePool{
		Name: "Net", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet,
		Description: "test",
	})

	// 10.0.0.0/24 is aligned on its natural boundary.
	score := svc.scoreAllocation("10.0.0.0/24", pool, 0)
	if score < 30 {
		t.Errorf("aligned /24 score = %d, expected >= 30 (alignment bonus)", score)
	}
	_ = st
}

func TestScoreAllocation_RFC1918(t *testing.T) {
	svc, _ := setupRecService(t)

	pool := domain.Pool{ID: 1, Name: "Public", CIDR: "8.8.8.0/24", Type: domain.PoolTypeSubnet}

	privateScore := svc.scoreAllocation("10.0.0.0/24", pool, 0)
	publicScore := svc.scoreAllocation("8.8.4.0/24", pool, 0)

	if privateScore <= publicScore {
		t.Errorf("RFC1918 score (%d) should be higher than public (%d)", privateScore, publicScore)
	}
}

func TestScoreAllocation_DesiredPrefixLen(t *testing.T) {
	svc, _ := setupRecService(t)

	pool := domain.Pool{ID: 1, Name: "Net", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet}

	exactMatch := svc.scoreAllocation("10.0.0.0/28", pool, 28)
	noMatch := svc.scoreAllocation("10.0.0.0/20", pool, 28)

	if exactMatch <= noMatch {
		t.Errorf("exact prefix len match (%d) should score higher than mismatch (%d)", exactMatch, noMatch)
	}
}
