package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestDriftDetector_UnmanagedResource(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)

	// Create an account.
	acct, err := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:111", Name: "Test Account", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}

	// Create a discovered resource with no linked pool.
	resID := uuid.New()
	err = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           resID,
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-12345",
		Name:         "prod-vpc",
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	detector := NewDriftDetector(ms, ds, driftStore)
	resp, err := detector.Detect(ctx, domain.RunDriftDetectionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected 1 drift item, got %d", resp.Total)
	}
	if resp.Items[0].Type != domain.DriftTypeUnmanaged {
		t.Fatalf("expected type %s, got %s", domain.DriftTypeUnmanaged, resp.Items[0].Type)
	}
	if resp.Items[0].Severity != domain.DriftSeverityWarning {
		t.Fatalf("expected severity %s, got %s", domain.DriftSeverityWarning, resp.Items[0].Severity)
	}
}

func TestDriftDetector_CIDRMismatch(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)

	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:222", Name: "Acct2", Provider: "aws"})

	// Create a pool.
	pool, _ := ms.CreatePool(ctx, domain.CreatePool{
		Name:      "prod-vpc",
		CIDR:      "10.0.0.0/16",
		AccountID: &acct.ID,
		Source:    domain.PoolSourceDiscovered,
	})

	// Create a discovered resource linked to the pool but with a different CIDR.
	resID := uuid.New()
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           resID,
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-abc",
		Name:         "prod-vpc",
		CIDR:         "10.1.0.0/16",
		PoolID:       &pool.ID,
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})

	detector := NewDriftDetector(ms, ds, driftStore)
	resp, err := detector.Detect(ctx, domain.RunDriftDetectionRequest{AccountIDs: []int64{acct.ID}})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected 1 drift item, got %d", resp.Total)
	}
	if resp.Items[0].Type != domain.DriftTypeCIDRMismatch {
		t.Fatalf("expected type %s, got %s", domain.DriftTypeCIDRMismatch, resp.Items[0].Type)
	}
	if resp.Items[0].Severity != domain.DriftSeverityCritical {
		t.Fatalf("expected severity %s, got %s", domain.DriftSeverityCritical, resp.Items[0].Severity)
	}
}

func TestDriftDetector_OrphanedPool(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)

	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:333", Name: "Acct3", Provider: "aws"})

	// Create a discovered-source pool with no active resource linked.
	_, _ = ms.CreatePool(ctx, domain.CreatePool{
		Name:      "orphan-vpc",
		CIDR:      "10.2.0.0/16",
		AccountID: &acct.ID,
		Source:    domain.PoolSourceDiscovered,
	})

	detector := NewDriftDetector(ms, ds, driftStore)
	resp, err := detector.Detect(ctx, domain.RunDriftDetectionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected 1 drift item, got %d", resp.Total)
	}
	if resp.Items[0].Type != domain.DriftTypeOrphanedPool {
		t.Fatalf("expected type %s, got %s", domain.DriftTypeOrphanedPool, resp.Items[0].Type)
	}
}

func TestDriftDetector_NoDrift(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)

	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:444", Name: "Acct4", Provider: "aws"})

	pool, _ := ms.CreatePool(ctx, domain.CreatePool{
		Name:      "matched-vpc",
		CIDR:      "10.3.0.0/16",
		AccountID: &acct.ID,
		Source:    domain.PoolSourceDiscovered,
	})

	// Linked resource with matching CIDR.
	resID := uuid.New()
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           resID,
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-match",
		Name:         "matched-vpc",
		CIDR:         "10.3.0.0/16",
		PoolID:       &pool.ID,
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})

	detector := NewDriftDetector(ms, ds, driftStore)
	resp, err := detector.Detect(ctx, domain.RunDriftDetectionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Total != 0 {
		t.Fatalf("expected 0 drift items, got %d", resp.Total)
	}
}

func TestDriftDetector_Idempotent(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)

	acct, _ := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:555", Name: "Acct5", Provider: "aws"})

	resID := uuid.New()
	_ = ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
		ID:           resID,
		AccountID:    acct.ID,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeSubnet,
		ResourceID:   "subnet-xyz",
		Name:         "web-subnet",
		CIDR:         "10.4.1.0/24",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	})

	detector := NewDriftDetector(ms, ds, driftStore)

	// Run twice — should not duplicate.
	_, _ = detector.Detect(ctx, domain.RunDriftDetectionRequest{})
	resp, err := detector.Detect(ctx, domain.RunDriftDetectionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected 1 drift item after re-detection, got %d", resp.Total)
	}

	// Verify the store has exactly 1 open item.
	items, total, _ := driftStore.ListDriftItems(ctx, domain.DriftFilters{Status: "open"})
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 open item in store, got %d", total)
	}
}

func TestCidrsEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"10.0.0.0/16", "10.0.0.0/16", true},
		{"10.0.0.0/16", "10.1.0.0/16", false},
		{"10.0.0.0/24", "10.0.0.0/25", false},
		{"invalid", "10.0.0.0/16", false},
		{"invalid", "invalid", true},
	}

	for _, tt := range tests {
		if got := cidrsEqual(tt.a, tt.b); got != tt.want {
			t.Errorf("cidrsEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
