package storage

import (
	"context"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// PlacementStore is the part of Store that changes which pools are live or what they hold: pool
// placement, resizing a pool, updating the metadata reclaim checks, and every delete that can
// remove a pool another placement might pick as a parent.
type PlacementStore interface {
	ListPools(ctx context.Context) ([]domain.Pool, error)
	GetPool(ctx context.Context, id int64) (domain.Pool, bool, error)
	CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error)
	UpdatePool(ctx context.Context, id int64, update domain.UpdatePool) (domain.Pool, bool, error)
	UpdatePoolCIDR(ctx context.Context, id int64, cidr string) (domain.Pool, bool, error)
	RestorePool(ctx context.Context, id int64) (domain.Pool, bool, error)
	DeletePool(ctx context.Context, id int64) (bool, error)
	DeletePoolCascade(ctx context.Context, id int64) (bool, error)
	DeleteAccountCascade(ctx context.Context, id int64) (bool, error)
}

var _ PlacementStore = Store(nil)

// PlacementLinks is the part of DiscoveryStore that code under the placement guard reads and
// writes: the resources linked to a pool, and linking a resource to a pool.
type PlacementLinks interface {
	PoolResourceIDs(ctx context.Context, poolID int64) ([]uuid.UUID, error)
	LinkResourceToPool(ctx context.Context, resourceID uuid.UUID, poolID int64) error
}

var _ PlacementLinks = DiscoveryStore(nil)

// LinksIn returns the link operations for code running under the placement guard that handed it
// ps. A store whose placement runs in a transaction provides them on ps, so they use the
// transaction's connection and commit or roll back with it. Otherwise they come from ds.
func LinksIn(ps PlacementStore, ds DiscoveryStore) PlacementLinks {
	if links, ok := ps.(PlacementLinks); ok {
		return links
	}
	return ds
}

// PlacementLocker is implemented by stores that several CloudPAM processes share.
//
// WithPlacementLock runs fn while holding a lock that serializes pool placement, resizes, restores
// and pool deletes for the store's organization across every process. fn sees every pool committed
// before it got the lock. What fn creates, resizes, restores or deletes is stored only when fn
// returns nil, ctx is still live, and the commit succeeds. Otherwise none of it is. fn's error is
// returned unchanged. The lock is not reentrant.
type PlacementLocker interface {
	WithPlacementLock(ctx context.Context, fn func(PlacementStore) error) error
}
