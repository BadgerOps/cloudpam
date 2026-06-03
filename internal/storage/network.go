package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// NetworkStore persists managed network objects and explicit relationships
// between pools, discovered resources, and managed network objects.
type NetworkStore interface {
	ListNetworkObjects(ctx context.Context, filters domain.NetworkObjectFilters) ([]domain.NetworkObject, error)
	GetNetworkObject(ctx context.Context, id int64) (domain.NetworkObject, bool, error)
	CreateNetworkObject(ctx context.Context, in domain.CreateNetworkObject) (domain.NetworkObject, error)
	UpdateNetworkObject(ctx context.Context, id int64, update domain.UpdateNetworkObject) (domain.NetworkObject, bool, error)

	ListNetworkRelationships(ctx context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error)
	UpsertNetworkRelationship(ctx context.Context, in domain.CreateNetworkRelationship) (domain.NetworkRelationship, error)
	UpdateNetworkRelationshipState(ctx context.Context, id string, state string, reason string) (domain.NetworkRelationship, bool, error)
}
