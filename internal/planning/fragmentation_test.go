package planning

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestAnalyzeFragmentation_NoChildren(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Empty",
		CIDR: "10.0.0.0/16",
		Type: domain.PoolTypeSupernet,
	})

	svc := NewAnalysisService(store)
	result, err := svc.AnalyzeFragmentation(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	if result.Score != 0 {
		t.Errorf("score = %d, want 0 for no children", result.Score)
	}
	if len(result.Issues) != 0 {
		t.Errorf("issues = %d, want 0", len(result.Issues))
	}
}

func TestAnalyzeFragmentation_Scattered(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Parent",
		CIDR: "10.0.0.0/16",
		Type: domain.PoolTypeSupernet,
	})

	// Create non-contiguous children to produce gaps.
	parentID := parent.ID
	store.CreatePool(ctx, domain.CreatePool{
		Name: "A", CIDR: "10.0.0.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})
	store.CreatePool(ctx, domain.CreatePool{
		Name: "B", CIDR: "10.0.10.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})
	store.CreatePool(ctx, domain.CreatePool{
		Name: "C", CIDR: "10.0.20.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	result, err := svc.AnalyzeFragmentation(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	if result.Score == 0 {
		t.Error("expected non-zero fragmentation score for scattered allocations")
	}

	// Should have a scattered issue.
	hasScattered := false
	for _, issue := range result.Issues {
		if issue.Type == FragmentScattered {
			hasScattered = true
		}
	}
	if !hasScattered {
		t.Error("expected scattered fragmentation issue")
	}
}

func TestAnalyzeFragmentation_MixedPrefixLengths(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Parent",
		CIDR: "10.0.0.0/16",
		Type: domain.PoolTypeSupernet,
	})

	parentID := parent.ID
	store.CreatePool(ctx, domain.CreatePool{
		Name: "Big", CIDR: "10.0.0.0/20", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})
	store.CreatePool(ctx, domain.CreatePool{
		Name: "Small", CIDR: "10.0.16.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})
	store.CreatePool(ctx, domain.CreatePool{
		Name: "Tiny", CIDR: "10.0.17.0/28", ParentID: &parentID, Type: domain.PoolTypeSubnet,
	})

	svc := NewAnalysisService(store)
	result, err := svc.AnalyzeFragmentation(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	hasMisaligned := false
	for _, issue := range result.Issues {
		if issue.Type == FragmentMisaligned {
			hasMisaligned = true
		}
	}
	if !hasMisaligned {
		t.Error("expected misaligned fragmentation issue for mixed prefix lengths")
	}
}

func TestAnalyzeFragmentation_NotFound(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	svc := NewAnalysisService(store)

	_, err := svc.AnalyzeFragmentation(ctx, 999)
	if err == nil {
		t.Error("expected error for non-existent pool")
	}
}
