package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// PlacementStore is the part of Store that pool placement reads and writes.
type PlacementStore interface {
	ListPools(ctx context.Context) ([]domain.Pool, error)
	CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error)
}

var _ PlacementStore = Store(nil)

// PlacementLocker is implemented by stores that several CloudPAM processes share.
//
// WithPlacementLock runs fn while holding a lock that serializes pool placement for the store's
// organization across every process. fn sees every pool committed before it got the lock. The pools
// fn creates are stored only when fn returns nil, ctx is still live, and the commit succeeds.
// Otherwise none of them are. fn's error is returned unchanged. The lock is not reentrant.
type PlacementLocker interface {
	WithPlacementLock(ctx context.Context, fn func(PlacementStore) error) error
}
