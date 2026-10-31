package storage

import (
	"context"
	"fmt"
	"strconv"

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
	// relationship that references it as source or target, atomically, and
	// returns how many relationships were removed. It returns ErrNotFound when
	// the object does not exist and ErrConflict while child objects still name
	// it as their parent. Linked pools and discovered resources are left
	// untouched.
	DeleteNetworkObject(ctx context.Context, id int64) (removed int, err error)

	ListNetworkRelationships(ctx context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error)
	// UpsertNetworkRelationship creates or replaces a relationship. When the
	// source or target kind is NetworkEntityKindObject the object must exist,
	// else it returns ErrValidation; the check is atomic with
	// DeleteNetworkObject. Pool and discovered-resource references are not
	// checked.
	UpsertNetworkRelationship(ctx context.Context, in domain.CreateNetworkRelationship) (domain.NetworkRelationship, error)
	UpdateNetworkRelationshipState(ctx context.Context, id string, state string, reason string) (domain.NetworkRelationship, bool, error)
}

// NetworkRelationshipObjectIDs returns the ids of the network objects a
// relationship names as source or target, in that order. An id that is not a
// positive integer cannot name an object and yields NetworkObjectRefNotFound.
func NetworkRelationshipObjectIDs(in domain.CreateNetworkRelationship) ([]int64, error) {
	var ids []int64
	for _, ref := range [][2]string{{in.SourceKind, in.SourceID}, {in.TargetKind, in.TargetID}} {
		if ref[0] != domain.NetworkEntityKindObject {
			continue
		}
		id, err := strconv.ParseInt(ref[1], 10, 64)
		if err != nil || id < 1 {
			return nil, NetworkObjectRefNotFound(ref[1])
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// NetworkObjectRefNotFound is the ErrValidation a backend returns when a
// relationship names a network object that does not exist.
func NetworkObjectRefNotFound(id string) error {
	return fmt.Errorf("network object %s not found: %w", id, ErrValidation)
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
