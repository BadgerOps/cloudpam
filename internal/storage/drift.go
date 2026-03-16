package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// DriftStore provides storage operations for drift detection results.
type DriftStore interface {
	// CreateDriftItem persists a new drift item.
	CreateDriftItem(ctx context.Context, item domain.DriftItem) error

	// GetDriftItem returns a single drift item by ID.
	GetDriftItem(ctx context.Context, id string) (*domain.DriftItem, error)

	// ListDriftItems returns paginated drift items matching the filters.
	ListDriftItems(ctx context.Context, filters domain.DriftFilters) ([]domain.DriftItem, int, error)

	// UpdateDriftStatus updates a drift item's status and optional ignore reason.
	UpdateDriftStatus(ctx context.Context, id string, status domain.DriftStatus, ignoreReason string) error

	// DeleteOpenForAccount removes all open drift items for an account (before re-detection).
	DeleteOpenForAccount(ctx context.Context, accountID int64) error
}
