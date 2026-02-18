package storage

import (
	"context"
	"time"

	"cloudpam/internal/domain"
)

// UtilizationStore provides storage operations for pool utilization snapshots.
type UtilizationStore interface {
	// RecordSnapshot captures the current utilization of a pool.
	RecordSnapshot(ctx context.Context, snap domain.UtilizationSnapshot) error

	// ListSnapshots returns snapshots for a pool within a time range, ordered by captured_at ASC.
	ListSnapshots(ctx context.Context, poolID int64, from, to time.Time) ([]domain.UtilizationSnapshot, error)

	// LatestSnapshot returns the most recent snapshot for a pool, or nil if none exist.
	LatestSnapshot(ctx context.Context, poolID int64) (*domain.UtilizationSnapshot, error)
}
