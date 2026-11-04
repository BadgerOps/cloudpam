package storage

import (
	"context"
	"time"

	"cloudpam/internal/domain"
)

// SyncScheduleStore persists schedule settings and coordinates server syncs across replicas.
type SyncScheduleStore interface {
	GetSyncSchedule(context.Context, int64) (domain.SyncSchedule, error)
	PutSyncSchedule(context.Context, int64, int64, time.Time) error
	DueSyncSchedules(context.Context, time.Time) ([]domain.SyncSchedule, error)
	// ClaimSync atomically acquires a lease. Scheduled claims also require an enabled, due schedule.
	ClaimSync(ctx context.Context, accountID int64, token string, now, until time.Time, scheduled bool) (bool, error)
	ReleaseSync(ctx context.Context, accountID int64, token string) error
	RecordSyncAgent(ctx context.Context, accountID int64, agentID string) error
}
