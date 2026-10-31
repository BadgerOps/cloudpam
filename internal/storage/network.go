package storage

import (
	"context"
	"fmt"

	"cloudpam/internal/domain"
)

// NetworkStore persists managed network objects and explicit relationships
// between pools, discovered resources, and managed network objects.
type NetworkStore interface {
	ListNetworkObjects(ctx context.Context, filters domain.NetworkObjectFilters) ([]domain.NetworkObject, error)
	GetNetworkObject(ctx context.Context, id int64) (domain.NetworkObject, bool, error)
	CreateNetworkObject(ctx context.Context, in domain.CreateNetworkObject) (domain.NetworkObject, error)
	UpdateNetworkObject(ctx context.Context, id int64, update domain.UpdateNetworkObject) (domain.NetworkObject, bool, error)
	// DeleteNetworkObject removes an object together with every network
	// relationship that references it as source or target, atomically. It
	// returns ErrNotFound when the object does not exist and ErrConflict while
	// child objects still name it as their parent. Linked pools and discovered
	// resources are left untouched.
	DeleteNetworkObject(ctx context.Context, id int64) error

	ListNetworkRelationships(ctx context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error)
	UpsertNetworkRelationship(ctx context.Context, in domain.CreateNetworkRelationship) (domain.NetworkRelationship, error)
	UpdateNetworkRelationshipState(ctx context.Context, id string, state string, reason string) (domain.NetworkRelationship, bool, error)
}

// NetworkObjectHasChildrenError returns the ErrConflict a backend reports when
// a network object still has child objects, or nil when it has none.
func NetworkObjectHasChildrenError(children int) error {
	if children == 0 {
		return nil
	}
	noun := "child objects"
	if children == 1 {
		noun = "child object"
	}
	return fmt.Errorf("%w: network object has %d %s; delete or re-parent them first", ErrConflict, children, noun)
}
