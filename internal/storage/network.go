package storage

import (
	"context"
	"fmt"
	"strings"

	"cloudpam/internal/domain"
)

// NetworkStore persists managed network objects and explicit relationships
// between pools, discovered resources, and managed network objects.
type NetworkStore interface {
	ListNetworkObjects(ctx context.Context, filters domain.NetworkObjectFilters) ([]domain.NetworkObject, error)
	GetNetworkObject(ctx context.Context, id int64) (domain.NetworkObject, bool, error)
	CreateNetworkObject(ctx context.Context, in domain.CreateNetworkObject) (domain.NetworkObject, error)
	UpdateNetworkObject(ctx context.Context, id int64, update domain.UpdateNetworkObject) (domain.NetworkObject, bool, error)
	// DeleteNetworkObject removes an object. It returns ErrNotFound when the
	// object does not exist and ErrConflict while child objects or network
	// relationships still refer to it. Linked pools and discovered resources
	// are left untouched.
	DeleteNetworkObject(ctx context.Context, id int64) error

	ListNetworkRelationships(ctx context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error)
	UpsertNetworkRelationship(ctx context.Context, in domain.CreateNetworkRelationship) (domain.NetworkRelationship, error)
	UpdateNetworkRelationshipState(ctx context.Context, id string, state string, reason string) (domain.NetworkRelationship, bool, error)
}

// NetworkObjectInUseError returns the ErrConflict a backend reports when a
// network object still has dependants, or nil when it has none.
func NetworkObjectInUseError(children, relationships int) error {
	var reasons []string
	if children > 0 {
		reasons = append(reasons, fmt.Sprintf("has %d child %s", children, plural(children, "object", "objects")))
	}
	if relationships > 0 {
		reasons = append(reasons, fmt.Sprintf("is referenced by %d %s", relationships, plural(relationships, "relationship", "relationships")))
	}
	if len(reasons) == 0 {
		return nil
	}
	return fmt.Errorf("%w: network object %s", ErrConflict, strings.Join(reasons, " and "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
