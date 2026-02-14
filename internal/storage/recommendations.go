package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// RecommendationStore provides storage operations for recommendations.
type RecommendationStore interface {
	// CreateRecommendation persists a new recommendation.
	CreateRecommendation(ctx context.Context, rec domain.Recommendation) error

	// GetRecommendation returns a single recommendation by ID.
	GetRecommendation(ctx context.Context, id string) (*domain.Recommendation, error)

	// ListRecommendations returns paginated recommendations matching the filters.
	ListRecommendations(ctx context.Context, filters domain.RecommendationFilters) ([]domain.Recommendation, int, error)

	// UpdateRecommendationStatus updates a recommendation's status and optional fields.
	UpdateRecommendationStatus(ctx context.Context, id string, status domain.RecommendationStatus, dismissReason string, appliedPoolID *int64) error

	// DeletePendingForPool removes all pending recommendations for a pool (before regeneration).
	DeletePendingForPool(ctx context.Context, poolID int64) error
}
