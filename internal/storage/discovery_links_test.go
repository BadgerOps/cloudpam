package storage

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func TestMemoryDiscoveryStore_PoolResourceIDs(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	ds := NewMemoryDiscoveryStore(m)
	pool, other := int64(7), int64(8)
	now := time.Now().UTC()
	linked, elsewhere, unlinked := uuid.New(), uuid.New(), uuid.New()
	for _, r := range []domain.DiscoveredResource{
		{ID: linked, AccountID: 1, ResourceID: "vpc-a", CIDR: "10.0.0.0/24", PoolID: &pool, Status: domain.DiscoveryStatusActive, LastSeenAt: now},
		{ID: elsewhere, AccountID: 1, ResourceID: "vpc-b", CIDR: "10.0.1.0/24", PoolID: &other, Status: domain.DiscoveryStatusActive, LastSeenAt: now},
		{ID: unlinked, AccountID: 1, ResourceID: "vpc-c", CIDR: "10.0.2.0/24", Status: domain.DiscoveryStatusActive, LastSeenAt: now},
	} {
		if err := ds.UpsertDiscoveredResource(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := ds.PoolResourceIDs(ctx, pool)
	if err != nil || !slices.Equal(ids, []uuid.UUID{linked}) {
		t.Fatalf("PoolResourceIDs(%d) = %v err=%v, want [%s]", pool, ids, err, linked)
	}
	if ids, err := ds.PoolResourceIDs(ctx, 99); err != nil || len(ids) != 0 {
		t.Fatalf("PoolResourceIDs(99) = %v err=%v, want none", ids, err)
	}
}
