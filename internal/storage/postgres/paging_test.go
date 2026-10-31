//go:build postgres

package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// Each list must treat a page near MaxInt as past the end: (page-1)*pageSize
// used to wrap negative and go into OFFSET, which PostgreSQL rejects as a
// negative OFFSET, so the list failed instead of coming back empty.
func TestLists_PageNearMaxIntIsEmpty(t *testing.T) {
	resetDB(t)
	s := testDB.store
	ctx := context.Background()
	account, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:111111111111", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "prod", CIDR: "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	now := time.Now().UTC()
	for i := range 3 {
		at := now.Add(time.Duration(i) * time.Minute)
		if err := s.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{ID: uuid.New(), AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-" + string(rune('a'+i)), CIDR: "10.70.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: at, LastSeenAt: at}); err != nil {
			t.Fatalf("upsert discovered resource: %v", err)
		}
		if err := s.CreateDriftItem(ctx, domain.DriftItem{ID: "d-" + string(rune('a'+i)), AccountID: account.ID, Type: domain.DriftTypeUnmanaged, Severity: domain.DriftSeverityWarning, Status: domain.DriftStatusOpen, Title: "drift", DetectedAt: at, UpdatedAt: at}); err != nil {
			t.Fatalf("create drift item: %v", err)
		}
		if err := s.CreateRecommendation(ctx, domain.Recommendation{ID: "r-" + string(rune('a'+i)), PoolID: pool.ID, Type: domain.RecommendationTypeAllocation, Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh, Title: "rec", SuggestedCIDR: "10.0.5.0/24", CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatalf("create recommendation: %v", err)
		}
	}

	for _, page := range []int{math.MaxInt, math.MaxInt/200 + 2} {
		items, total, err := s.ListDiscoveredResources(ctx, account.ID, domain.DiscoveryFilters{Page: page, PageSize: 2})
		if err != nil {
			t.Fatalf("discovered page=%d: %v", page, err)
		}
		if total != 3 || len(items) != 0 {
			t.Errorf("discovered page=%d: total=%d len=%d, want 3/0", page, total, len(items))
		}
		drifts, total, err := s.ListDriftItems(ctx, domain.DriftFilters{Page: page, PageSize: 2})
		if err != nil {
			t.Fatalf("drift page=%d: %v", page, err)
		}
		if total != 3 || len(drifts) != 0 {
			t.Errorf("drift page=%d: total=%d len=%d, want 3/0", page, total, len(drifts))
		}
		recs, total, err := s.ListRecommendations(ctx, domain.RecommendationFilters{Page: page, PageSize: 2})
		if err != nil {
			t.Fatalf("recommendations page=%d: %v", page, err)
		}
		if total != 3 || len(recs) != 0 {
			t.Errorf("recommendations page=%d: total=%d len=%d, want 3/0", page, total, len(recs))
		}
	}
}
