//go:build sqlite

package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func TestPoolResourceIDs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	account, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:links", Name: "links", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "linked", CIDR: "10.80.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	linked, unlinked := uuid.New(), uuid.New()
	for _, r := range []domain.DiscoveredResource{
		{ID: linked, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-a", CIDR: "10.80.0.0/24", PoolID: &pool.ID, Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
		{ID: unlinked, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-b", CIDR: "10.81.0.0/24", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now},
	} {
		if err := s.UpsertDiscoveredResource(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := s.PoolResourceIDs(ctx, pool.ID)
	if err != nil || !slices.Equal(ids, []uuid.UUID{linked}) {
		t.Fatalf("PoolResourceIDs(%d) = %v err=%v, want [%s]", pool.ID, ids, err, linked)
	}
	if ids, err := s.PoolResourceIDs(ctx, pool.ID+100); err != nil || len(ids) != 0 {
		t.Fatalf("PoolResourceIDs(unknown) = %v err=%v, want none", ids, err)
	}
}
