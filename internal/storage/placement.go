package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// PlacementStore is the part of Store that changes which pools are live: pool placement and
// every delete that can remove a pool another placement might pick as a parent.
type PlacementStore interface {
	ListPools(ctx context.Context) ([]domain.Pool, error)
	CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error)
	RestorePool(ctx context.Context, id int64) (domain.Pool, bool, error)
	DeletePool(ctx context.Context, id int64) (bool, error)
	DeletePoolCascade(ctx context.Context, id int64) (bool, error)
	DeleteAccountCascade(ctx context.Context, id int64) (bool, error)
}

var _ PlacementStore = Store(nil)

// PlacementLocker is implemented by stores that several CloudPAM processes share.
//
// WithPlacementLock runs fn while holding a lock that serializes pool placement, restores and pool
// deletes for the store's organization across every process. fn sees every pool committed before it
// got the lock. What fn creates, restores or deletes is stored only when fn returns nil, ctx is still
// live, and the commit succeeds. Otherwise none of it is. fn's error is returned unchanged. The lock
// is not reentrant.
type PlacementLocker interface {
	WithPlacementLock(ctx context.Context, fn func(PlacementStore) error) error
}
